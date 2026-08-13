# Agent Vault Domain

Agent Vault brokers authenticated outbound requests for agents without exposing vault-stored credentials to them. This glossary fixes the language used for self-rotating long-lived agent tokens.

## Agent Tokens

**Agent identity**:
The stable actor represented by an agent record. Its instance role, vault grants, status, name, and audit attribution do not change when its token changes.
_Avoid_: Token family, credential

**Current agent token**:
The single long-lived `av_agt_` bearer token presently authorized to authenticate an agent identity.
_Avoid_: Generation, access token

**Renew**:
Let the current agent token replace itself with a new current token for the same agent identity.
_Avoid_: Refresh, rotate

**Rotate**:
Let an authorized operator replace an agent's current token without changing the agent identity.
_Avoid_: Renew, reissue
