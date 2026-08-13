# Self-rotating Agent Tokens

Status: Proposed

## Summary

Agent Vault should allow the current long-lived agent token, whose prefix is `av_agt_`, to replace itself without requiring a user login session. The replacement remains an instance-level agent token for the same agent. Its permissions continue to come from the agent's instance role and vault grants.

Each agent stores the hash of its single current token. A token authenticates only when its hash matches that pointer. Both self-renewal and the existing operator-authorized `agent rotate` operation atomically replace the pointer with a new token hash. Old token hashes remain in `sessions` temporarily so obsolete-token presentation can be recognized and audited.

This design does not change `av_sess_` user sessions or temporary vault-scoped sessions created by `vault run` and `vault token`. It introduces neither token families nor token generation numbers.

## Target Credential

The target is the token returned by `agent-vault agent create`, not the temporary token minted from a user session by `agent-vault vault token`.

| Token | Prefix | Identity and scope | Current lifetime |
| --- | --- | --- | --- |
| Agent token | `av_agt_` | Identifies an instance-level agent; permissions resolve from the agent's current roles and vault grants | No expiry |
| Vault-scoped session | `av_sess_` | Bound to one vault and a stored vault role | 5 minutes to 7 days |
| User login session | `av_sess_` | Identifies a human user and inherits that user's permissions | 1 year absolute, 30-day idle |

An agent token already references `agent_id`, so the agent is the stable identity around which credentials change. The remaining state the server needs is not an ordinal generation; it is the identity of the one token currently authorized for that agent.

## Context

Creating an agent currently creates three kinds of state atomically:

- An `agents` row containing the stable agent identity and instance role.
- Zero or more `vault_grants` rows containing the agent's vault roles.
- A non-expiring `sessions` row containing the SHA-256 hash of the returned `av_agt_` token and its `agent_id`.

The existing `POST /v1/agents/{name}/rotate` endpoint deletes every token for that agent and creates a fresh non-expiring token. The CLI exposes it as `agent-vault agent rotate <name> --token-only`. Calling it requires a user or agent actor with instance `member` access that is either an owner or the agent's creator.

That endpoint is scriptable, but a machine running the agent cannot call it with its own normal `no-access` plus vault `proxy` identity. It must retain a broader user or management token merely to rotate its own credential.

## Goals

- Let the current `av_agt_` token replace itself without a user session.
- Keep one token value, one token prefix, and the existing `AGENT_VAULT_TOKEN` interface.
- Preserve the agent's identity, instance role, vault grants, and audit attribution across renewal.
- Make exactly one token current for an agent.
- Reject every previous token after replacement commits.
- Let existing operator-authorized rotation forcibly replace the current token.
- Retain old token hashes long enough to audit obsolete-token presentation.
- Preserve existing behavior for user sessions and vault-scoped sessions.

## Non-goals

- Add access and refresh token pairs.
- Change `av_agt_` into an `av_sess_` token.
- Bind an instance-level agent token to one vault.
- Change agent roles or vault grants during token renewal.
- Make all agent tokens proxy-only.
- Add expiry or an absolute renewal deadline to long-lived agent tokens in the first version.
- Prove that the current token has or has not been copied.
- Automatically revoke an agent after stale-token use.
- Recover a replacement token after a successful response is lost.
- Support several hosts or replicas concurrently renewing one shared token.
- Add JWT, signed claims, token-family identifiers, or generation counters.

## Permission Semantics

Renewal preserves identity, not a snapshot of permissions. The new token identifies the same `agent_id`; permission checks continue reading the agent's current instance role and vault grants.

For the low-privilege runtime discussed here, the intended configuration is:

```text
agent instance role: no-access
vault grant:          proxy
```

Such an agent can proxy configured services and create proposals in its granted vaults, but it cannot manage instance resources or reveal and modify credentials. If an operator grants that agent `member`, `admin`, or instance-level privileges, its current token immediately receives those privileges through the existing actor model. Renewal neither elevates nor reduces them.

The self-renewal endpoint must authorize the presented token by token type and current-token state. It must not call `requireInstanceMember`, require vault `member`, or accept any requested role.

## Domain Model

### Agent identity

The `agents` row is the stable principal. Its ID, name, status, instance role, creator, and vault grants survive both self-renewal and manual rotation.

### Current agent token

`agents.current_token_hash` is the SHA-256 hash of the only `av_agt_` token currently authorized for that agent. It is equal to the corresponding `sessions.id`, which already stores token hashes.

A token is current when:

```text
SHA256(presented_token) = agents.current_token_hash
```

This direct pointer expresses the authentication invariant more precisely than a generation counter. The system needs to know which token is current, not how many replacements preceded it.

### Retired agent token

An agent-token session whose hash no longer equals the agent's current pointer is retired. It never authenticates. Its row may be retained temporarily so presentation of an obsolete credential can be attributed to an agent.

Retained history is operational evidence, not permanent business data. A maintenance job may delete retired agent-token sessions after the request-log retention window, provided it never removes the current token. After cleanup, a sufficiently old token is still rejected but can only be classified as unknown and invalid instead of attributed to a specific agent.

## Security Invariants

The store implementation must enforce these invariants:

1. Each managed agent has at most one current token hash.
2. A current token hash points to a session belonging to that same agent.
3. An agent token authenticates only when the agent is active and its session hash equals `current_token_hash`.
4. The current session must also be unexpired if an expiry exists.
5. Self-renewal can replace only the current token that authenticated the request.
6. At most one concurrent replacement of a current token succeeds.
7. Renewal does not change the agent, its roles, grants, status, or token lifetime policy.
8. Manual rotation replaces the current token regardless of whether its raw value is available.
9. Revoking or deleting the agent invalidates every token immediately.
10. Raw token values are returned once; only SHA-256 hashes are persisted.

These checks belong behind transactional store methods. HTTP handlers must not assemble token replacement from independent reads and writes.

## Threat Model

### Current-token theft

Anyone holding the current bearer token can use every permission currently granted to the agent and can renew the token. The server cannot distinguish two callers presenting identical token bytes.

The theft remains invisible while both parties use the same current token. It becomes observable only after one party replaces the token or usage metadata reveals an anomaly.

### Renewal race

If the legitimate client renews first, the stolen token stops authenticating after the transaction commits. If the attacker renews first, the legitimate client loses the race and its token stops authenticating. The legitimate client cannot retrieve the attacker's replacement token and must ask an authorized operator to run `agent rotate`.

The compare-and-swap on `current_token_hash` ensures exactly one replacement succeeds, so concurrent renewal cannot create two current tokens.

### Obsolete-token presentation

Presentation of an old token is observable while its session hash remains stored. It is a security signal, not proof of theft. Other causes include an in-flight request, concurrent local processes, a retry after a lost response, or restored machine state.

Every non-current token is rejected. A short time window and source metadata may classify the event, but must never make the old token current again.

Suggested events:

| Condition | Event | Severity |
| --- | --- | --- |
| Same source shortly after replacement | `av.agent-token-stale` | Warning |
| Different source or delayed reuse | `av.agent-token-reuse` | High |
| Token presented after agent revocation | `av.revoked-agent-token-use` | High |

The initial implementation should not automatically revoke the agent. A delayed request carrying an old token must not be able to deny service to the legitimate runtime.

### No automatic expiry

This design preserves the current non-expiring agent-token behavior. A stolen current token remains usable until the legitimate runtime renews, an operator rotates, or the agent is revoked. Regular renewal reduces the practical exposure window, but no server-enforced deadline guarantees it.

Adding token expiry is a separable future hardening measure. It is intentionally excluded from the first implementation so this change remains focused on self-rotation and compromise recovery.

## Data Model

Add one nullable logical pointer to `agents`:

```sql
ALTER TABLE agents ADD COLUMN current_token_hash TEXT;

CREATE UNIQUE INDEX idx_agents_current_token_hash
    ON agents(current_token_hash)
    WHERE current_token_hash IS NOT NULL;
```

No new columns are required on `sessions`:

- `sessions.id` is already the SHA-256 hash of the raw token.
- `sessions.agent_id` already identifies the owning agent.
- `sessions.expires_at` already carries an optional absolute expiry.
- `sessions.created_at` already records issuance time for display and retention.

The unique partial index prevents two agents from pointing to the same session hash. A database foreign key from `agents.current_token_hash` to `sessions.id` is intentionally omitted: together with the existing `sessions.agent_id -> agents.id` relationship it would create a circular insert and delete dependency, especially awkward across SQLite and PostgreSQL. Creation, renewal, and rotation maintain the logical pointer inside transactions, and store tests enforce same-agent ownership.

### Existing agents

Existing databases may contain zero, one, or several active token rows for an agent because the store currently permits multiple agent sessions even though normal creation and rotation return one.

Migration must not guess which existing token should become current. Existing agents receive `current_token_hash = NULL`; their existing `av_agt_` tokens continue authenticating under legacy behavior but cannot self-renew. The next operator `agent rotate` creates a current-token-managed credential and retires all legacy agent tokens.

Newly created agents receive a current token pointer immediately.

This staged behavior avoids breaking existing deployments and gives every agent an explicit transition point.

## Initial Agent Creation

The existing creation interface stays structurally the same:

```http
POST /v1/agents
Authorization: Bearer <authorized-management-session>
```

`CreateAgentWithGrantsAndToken` additionally writes the new token hash to `agents.current_token_hash` in the same transaction as the agent, grants, and session.

The response remains compatible:

```json
{
  "av_agent_token": "av_agt_...",
  "name": "my-agent",
  "role": "no-access",
  "vaults": [
    {"vault_name": "default", "vault_role": "proxy"}
  ]
}
```

No hash, version, generation, or other internal pointer is exposed to the caller.

## Self-renewal Interface

The current agent token replaces itself through a narrow endpoint:

```http
POST /v1/agents/self/token/renew
Authorization: Bearer av_agt_...
```

The request has no body. The server derives the agent and expected current hash exclusively from the authenticated token.

Successful response:

```json
{
  "av_agent_token": "av_agt_...",
  "renewed_at": "2026-08-13T12:00:00Z"
}
```

The endpoint accepts only a current-token-managed agent token. User sessions, scoped sessions, legacy agent tokens, expired tokens, retired tokens, and tokens for revoked agents are rejected.

Suggested errors:

| Status | Code | Meaning |
| --- | --- | --- |
| `400` | `agent_token_not_renewable` | A valid legacy agent token has no current-token pointer. |
| `403` | `agent_token_required` | The authenticated credential is not an agent token. |
| `409` | `renewal_conflict` | Another replacement won after this request authenticated. |
| `401` | `invalid_session` | The token is expired, retired, revoked, or otherwise invalid. |

An already retired token normally fails in authentication and receives generic `401`. The `409` case exists only for concurrent requests that both authenticated while current and then raced in the replacement transaction.

## Renewal Transaction

Renewal should cross one store interface:

```go
RenewAgentToken(ctx context.Context, rawToken string, now time.Time) (*Session, error)
```

The implementation performs one transaction:

1. Hash and load the presented session.
2. Confirm it is an agent token and is not expired.
3. Load the agent and confirm it is active and current-token-managed.
4. Confirm the presented session belongs to the agent and its hash equals `current_token_hash`.
5. Generate a new raw `av_agt_` token and hash it.
6. Insert the new session for the same agent with the preserved expiry policy.
7. Replace the agent's current pointer using a compare-and-swap update.
8. Commit and return the raw replacement token once.

The compare-and-swap provides the single-winner guarantee:

```sql
UPDATE agents
SET current_token_hash = ?,
    updated_at = ?
WHERE id = ?
  AND status = 'active'
  AND current_token_hash = ?;
```

The arguments are the new hash, current time, agent ID, and presented old hash. Only `RowsAffected == 1` may commit. If another request changed the pointer first, the transaction rolls back the newly inserted session and returns `renewal_conflict`.

Current production agent tokens do not expire, so the replacement normally has `expires_at = NULL`. If an internally issued agent token has an absolute expiry, renewal copies that same absolute timestamp and never extends it. Renewal therefore preserves the existing lifetime policy instead of turning it into a sliding lifetime.

SQLite and PostgreSQL may use different locking mechanics, but both adapters must expose identical transaction results.

## Authentication Changes

Current-token-managed agent authentication adds these checks to normal token-hash and expiry validation:

```text
agent.status = active
AND session.agent_id = agent.id
AND session.id = agent.current_token_hash
```

Both control-plane `requireAuth` and proxy-side `StoreSessionResolver` must use current-pointer-aware resolution. After renewal or rotation commits, the previous token fails every new authentication attempt through both ingress paths. Requests that passed authentication before the commit are not interrupted.

Legacy agents with `current_token_hash IS NULL` retain their current authentication behavior until operator rotation moves them into the managed model.

Resolution should return typed internal failure reasons for audit capture while keeping the external response generic.

## Manual Rotation

The existing endpoint and CLI remain the operator recovery path:

```http
POST /v1/agents/{name}/rotate
Authorization: Bearer <authorized-management-session>
```

```bash
agent-vault agent rotate my-agent --token-only
```

`RotateAgentToken` changes from deleting token history to performing one transaction:

1. Load and lock the agent's current pointer.
2. Generate a new raw `av_agt_` token and hash it.
3. Insert the new session for that agent.
4. Replace `current_token_hash` with the new hash using the observed pointer as a compare-and-swap condition.
5. Preserve the route's existing agent-reactivation behavior.
6. Commit and return the new raw token.

All old tokens become invalid because their hashes no longer match the pointer. Their rows remain temporarily available for audit classification.

Operator rotation is authoritative over concurrent self-renewal. If its compare-and-swap loses to a renewal, the store rolls back the unreturned token, reloads the latest pointer, and retries the rotation transaction. The method returns only after the token returned to the operator is the agent's current token, or after a real storage error. A bounded internal retry is sufficient because each conflict represents another committed pointer change; callers do not participate in this protocol.

Manual rotation is used when:

- Token compromise is suspected.
- An attacker or competing process may have won self-renewal.
- A renewal response was lost.
- The current token is unavailable.
- A legacy agent needs to enter the current-token-managed model.

Revoking the agent remains stronger than rotation: it disables the identity rather than replacing only its credential.

`RevokeAgent` should stop deleting agent-token rows immediately. It marks the agent revoked in the same transaction so every token fails the status check and later presentation can be classified as revoked-agent token use. Scoped sessions that the agent minted on behalf of other callers should still be deleted as they are today. `DeleteAgent` remains the permanent cleanup operation and may remove token history through the existing foreign-key cascade.

## CLI Design

Add a runtime-facing renewal command distinct from the management-facing rotate command:

```bash
agent-vault agent token renew
```

It reads:

```text
AGENT_VAULT_ADDR
AGENT_VAULT_TOKEN
```

and prints only the replacement `av_agt_` token to stdout. The token must not be accepted as a command-line flag because process listings and shell history may expose it.

A shell can replace its environment value explicitly:

```bash
export AGENT_VAULT_TOKEN="$(agent-vault agent token renew)"
```

For persistent operation, token-file mode is safer:

```bash
agent-vault agent token renew \
  --token-file ~/.config/agent-vault/my-agent.token
```

The CLI reads the old token, calls renewal, writes the replacement to a mode-`0600` temporary file in the same directory, syncs it, and atomically renames it over the old file. A failed request must never truncate or replace the current file.

The existing command keeps its management meaning:

```bash
agent-vault agent rotate my-agent --token-only
```

It uses the saved management session, forcibly replaces the current pointer, and is the recovery path when self-renewal cannot complete.

## Failure Modes

### Lost renewal response

The server may commit the new pointer while the response containing the raw token is lost. The client then knows only the retired token; the server stores only the replacement hash and cannot replay the raw value.

The first version deliberately does not add idempotency secrets, encrypted token recovery, pending replacements, or a dual-current grace period. Recovery is operator `agent rotate`.

### Concurrent renewal

If two processes renew the same current token concurrently, exactly one compare-and-swap succeeds. The loser cannot retrieve the winner's token and requires manual rotation unless it shares the winner's updated token file.

One agent token should therefore have one local owner. Multiple hosts, Kubernetes replicas, or CI runners should each use a different named agent and token rather than share one agent token.

### In-flight requests

A request may authenticate with the old token just before replacement commits. It may finish afterward. This does not violate the authentication invariant: the old token cannot begin any new authenticated request after the commit.

The audit classifier should allow for this race when deciding whether old-token use is merely stale or high risk.

### Retired-token retention

Keeping every retired token hash forever would grow `sessions` without bound for frequently renewed agents. The maintenance path should remove retired agent-token sessions after the same configurable retention period used for request logs, or another explicitly documented retention period.

Cleanup identifies current tokens by joining `sessions.id` to `agents.current_token_hash`; it must never rely only on age. Deleting retired rows affects audit attribution only, not token validity.

Existing token-count and latest-token queries must also become pointer-aware. For a managed agent, a retained retired session is not active: `CountAgentTokens` returns one only when the pointed-to session exists and is unexpired, otherwise zero; `GetLatestAgentTokenExpiry` reads only that session. For a legacy agent whose pointer is null, both methods retain their existing all-session behavior until operator rotation moves the agent into the managed model. History retention must not change operator-facing active-token counts.

## Alternatives Considered

### Continue using operator rotation

The command already exists and is scriptable, but it requires a broader management session on the runtime host. That is the credential exposure this proposal removes.

### Store a generation counter

Generation counters encode the current token indirectly: both the agent and every token row need coordinated ordinal values. The actual invariant is token identity, so a direct hash pointer requires fewer columns, avoids migration guesses and integer lifecycle concerns, and provides an equally strong compare-and-swap key.

### Add a token family ID

One agent currently has one active credential lineage. `agent_id` already identifies that lineage. A separate family becomes useful only if one agent may hold several independently rotating credentials, such as one per host or deployment. The current design instead recommends a distinct named agent for each independent runtime.

### Store `active` on session rows

A partial unique index could enforce one active session per agent, but it spreads current-state ownership across token-history rows and makes legacy migration ambiguous. A pointer on the stable agent identity directly answers which token is current.

### Use JWT or another self-contained signed token

A signed token could carry `agent_id` and a version claim, but immediate replacement would still require comparing that claim with server-side current state. Agent status, roles, and vault grants also remain dynamic database state. JWT therefore removes no required lookup while adding signing-key storage, key rotation, `kid` handling, global forgery impact if the signing key leaks, and a new dependency. The existing opaque 256-bit token plus SHA-256 pointer is simpler and has a smaller blast radius.

### Delete old token rows

The current manual rotation does this. Deletion invalidates credentials but discards the information needed to recognize an obsolete token later. Retaining hashes for a bounded period provides audit signals without retaining raw secrets.

## Implementation Shape

Keep the external store interface small and transactional:

```go
CreateAgentWithGrantsAndToken(...) (*Agent, *Session, error)
RenewAgentToken(ctx context.Context, rawToken string, now time.Time) (*Session, error)
RotateAgentToken(ctx context.Context, agentID string, expiresAt *time.Time) (*Session, error)
```

Creation, renewal, and rotation own their complete pointer transitions. Handlers only validate transport input and management authorization. Authentication uses one current-pointer-aware session-resolution interface shared by the control plane and proxy ingress.

Likely implementation areas:

- `internal/store`: migration, agent field, transactional creation/renewal/rotation, retired-token cleanup, and adapter tests.
- `internal/server/handle_agents.go`: self-renewal handler, existing rotation behavior, and audit events.
- `internal/server/server.go`: route registration and current-pointer-aware control-plane authentication.
- `internal/brokercore/session.go`: current-pointer-aware proxy authentication.
- `cmd/agent.go`: runtime renewal command and atomic token-file replacement.
- `web/src/pages/home/AllAgentsTab.tsx`: stale-token warnings after the server behavior ships.
- `skills/agent-vault-cli` and public docs: lifecycle and recovery instructions after implementation.

## Verification Plan

Store tests should prove:

- New agent creation atomically creates a token session and points the agent to its hash.
- Legacy agents with a null pointer remain valid but cannot self-renew.
- Renewal preserves `agent_id`, token prefix, and absolute expiry policy.
- Renewal replaces the pointer and makes the old token fail authentication.
- Two concurrent renewals produce exactly one committed replacement.
- Expired, retired, legacy, and revoked-agent tokens cannot renew.
- Manual rotation replaces the pointer and retires every previous token.
- Manual rotation wins over a concurrent renewal and returns the token that remains current.
- Agent revocation and deletion invalidate every token.
- The current pointer always references a token session belonging to the same agent.
- Retired history does not inflate active-token counts or latest-expiry results.
- Retired-token cleanup never removes a current token and degrades only audit attribution.
- Only token hashes, never raw values, are persisted.
- SQLite and PostgreSQL adapters expose identical results.

Server and proxy tests should prove:

- The pointed-to `av_agt_` token authenticates through control-plane and proxy ingress.
- Non-current tokens fail through both ingress paths after renewal or rotation.
- User and scoped sessions cannot call agent-token self-renewal.
- Renewal cannot alter roles, grants, agent identity, vault selection, or token lifetime.
- Obsolete-token use emits an audit event without exposing current-token state externally.
- A `no-access` agent with only a `proxy` vault grant remains unable to call member and owner operations after renewal.

CLI tests should prove:

- Renewal reads `AGENT_VAULT_TOKEN` without exposing it in arguments.
- Successful stdout contains only the new `av_agt_` token.
- Token-file mode preserves `0600` and replaces the file atomically.
- Failure never truncates or overwrites the current token file.
- Invalid or retired tokens provide actionable `agent rotate` recovery guidance.

## Rollout

1. Add nullable `agents.current_token_hash` and pointer-aware authentication without changing legacy tokens.
2. Make new agent creation and operator rotation populate the pointer.
3. Add agent-token self-renewal and focused store/server/proxy tests.
4. Add the CLI renewal and atomic token-file workflow.
5. Add bounded retired-token retention and obsolete-token audit events.
6. Update the agent skill, security documentation, CLI reference, and agent guides.

No migration selects a current token from existing agent sessions. Existing agents enter the managed model on their next operator-authorized rotation.
