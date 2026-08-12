# Proposals

Read this reference only when the user explicitly asks you to create, inspect,
or follow a proposal. A proposal changes Agent Vault configuration after human
approval; an authentication failure alone is not authorization to create one.

## Before creating

1. Inspect the failed request and its `proposal_hint` when present.
2. Determine the exact upstream host and how the upstream service authenticates.
3. Run `agent-vault vault proposal create --help` for the interface supported by
   the installed CLI version.
4. Check `agent-vault vault discover --json` when you need to avoid requesting a
   service or credential that is already available.
5. Keep the request to the minimum host, path scope, and credentials needed for
   the user's task.

Never include a real credential value in a proposal unless the user explicitly
asks you to provide that value and it is already available through an approved
secure source. Normally, declare the credential slot and let the human provide
its value during approval.

## Common proposals

Prefer flags for a single service or credential:

```bash
agent-vault vault proposal create \
  --name stripe \
  --host api.stripe.com \
  --auth-type bearer \
  --token-key STRIPE_KEY \
  --credential STRIPE_KEY="Stripe API key" \
  --message "Need Stripe access" \
  --json
```

```bash
agent-vault vault proposal create \
  --credential DB_PASSWORD="Database password" \
  --message "Need database access" \
  --json
```

Use JSON input only for features the flags do not express, such as multiple
services, custom authentication, substitutions, deletion, or OAuth:

```bash
agent-vault vault proposal create -f proposal.json --json
```

Do not infer the complete JSON shape from examples in this file. Follow the
installed CLI's help and validation errors until a versioned schema command is
available.

## Authentication mapping

- Bearer token: `--auth-type bearer --token-key CREDENTIAL_KEY`
- Basic authentication: `--auth-type basic --username-key USER_KEY` and,
  when required, `--password-key PASSWORD_KEY`
- API key header: `--auth-type api-key --api-key-key CREDENTIAL_KEY`, with
  `--api-key-header` and `--api-key-prefix` when the upstream requires them
- Passthrough: `--auth-type passthrough`; this allowlists the host without
  injecting a credential
- Custom headers, substitutions, and OAuth credentials require JSON input

Look up the upstream service's current official authentication instructions
before choosing an authentication type or OAuth endpoints. Do not guess them.

## Substitutions

Use substitutions only when the credential appears in a URL path, query,
request header, body, or WebSocket text frame instead of a standard auth field.
Scope each substitution to the required surfaces. Set `env` only when the
client must receive a placeholder through that exact environment variable.

Supported surfaces are `path`, `query`, `header`, `body`, and `websocket`.
Omitting `in` defaults to `path` and `query`; header replacement is deliberately
opt-in.

## After creating

1. Tell the user what access was requested and present the returned
   `approval_url`.
2. Poll only when the user asked you to follow the proposal. Use
   `agent-vault vault proposal get <id> --json` every 3 seconds for the first 30
   seconds, then every 10 seconds, stopping after 10 minutes.
3. Stop immediately on `rejected` or `expired` and report the status.
4. On `applied`, retry the original request once.
5. If the retry returns `502` with `oauth_not_connected`, ask the user to finish
   the OAuth connection in Agent Vault. Do not retry in a loop.

Never approve a proposal on the user's behalf unless the user explicitly asks
you to perform that separate action and has authorized the proposed changes.
