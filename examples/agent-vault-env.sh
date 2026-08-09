#!/bin/sh
# Copy this file, remove the exports your clients do not use, then source it:
#   . ./agent-vault-env.sh
# Set AGENT_VAULT_MITM_ADDR first when the server does not use port 14322.

agent_vault_env() {
	: "${AGENT_VAULT_ADDR:=http://127.0.0.1:14321}"
	: "${AGENT_VAULT_MITM_ADDR:=127.0.0.1:14322}"
	: "${AGENT_VAULT_CA_FILE:=$HOME/.agent-vault/mitm-ca.pem}"
	if [ -z "${AGENT_VAULT_VAULT:-}" ]; then
		AGENT_VAULT_VAULT="$(agent-vault vault current)" || return 1
	fi

	if [ -z "${AGENT_VAULT_TOKEN:-}" ]; then
		AGENT_VAULT_TOKEN="$(
			agent-vault vault token \
				--address "$AGENT_VAULT_ADDR" \
				--vault "$AGENT_VAULT_VAULT"
		)" || return 1
	fi

	mkdir -p "$(dirname "$AGENT_VAULT_CA_FILE")" || return 1
	agent-vault ca fetch \
		--address "$AGENT_VAULT_ADDR" \
		--output "$AGENT_VAULT_CA_FILE" || return 1

	agent_vault_proxy_url="http://${AGENT_VAULT_TOKEN}:${AGENT_VAULT_VAULT}@${AGENT_VAULT_MITM_ADDR}"
	agent_vault_placeholder_script="$(
		AGENT_VAULT_TOKEN="$AGENT_VAULT_TOKEN" \
			AGENT_VAULT_ADDR="$AGENT_VAULT_ADDR" \
			AGENT_VAULT_VAULT="$AGENT_VAULT_VAULT" \
			agent-vault placeholders
	)" || return 1
	agent_vault_no_proxy="localhost,127.0.0.1"
	if [ -n "${NO_PROXY:-}" ]; then
		agent_vault_no_proxy="${NO_PROXY},${agent_vault_no_proxy}"
	fi

	export AGENT_VAULT_ADDR
	export AGENT_VAULT_TOKEN
	export AGENT_VAULT_VAULT
	export HTTPS_PROXY="$agent_vault_proxy_url"
	export HTTP_PROXY="$agent_vault_proxy_url"
	export NO_PROXY="$agent_vault_no_proxy"

	# Keep only the trust variables used by your clients.
	export SSL_CERT_FILE="$AGENT_VAULT_CA_FILE"
	export CURL_CA_BUNDLE="$AGENT_VAULT_CA_FILE"
	export REQUESTS_CA_BUNDLE="$AGENT_VAULT_CA_FILE"
	export GIT_SSL_CAINFO="$AGENT_VAULT_CA_FILE"

	# Node.js:
	# export NODE_EXTRA_CA_CERTS="$AGENT_VAULT_CA_FILE"
	# export NODE_USE_ENV_PROXY=1

	# Deno:
	# export DENO_CERT="$AGENT_VAULT_CA_FILE"

	# OpenClaw:
	# export OPENCLAW_PROXY_URL="$agent_vault_proxy_url"

	eval "$agent_vault_placeholder_script" || return 1
	unset agent_vault_no_proxy agent_vault_placeholder_script agent_vault_proxy_url
}

agent_vault_env
agent_vault_env_status=$?
unset -f agent_vault_env
if [ "$agent_vault_env_status" -ne 0 ]; then
	return "$agent_vault_env_status" 2>/dev/null || exit "$agent_vault_env_status"
fi
unset agent_vault_env_status
