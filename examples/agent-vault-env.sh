#!/bin/sh
# Copy this file, review the exports your clients need, then source it:
#   . ./agent-vault-env.sh
# The Agent Vault CA must already be installed in the native system trust store.

agent_vault_env() {
	: "${AGENT_VAULT_ADDR:=http://127.0.0.1:14321}"
	: "${AGENT_VAULT_MITM_ADDR:=127.0.0.1:14322}"
	: "${AGENT_VAULT_NO_PROXY:=${NO_PROXY:+$NO_PROXY,}localhost,127.0.0.1}"
	export AGENT_VAULT_ADDR

	if [ -z "${AGENT_VAULT_VAULT:-}" ]; then
		if ! AGENT_VAULT_VAULT="$(agent-vault vault current)"; then
			AGENT_VAULT_VAULT="default"
			echo 'HINT: Use "default" vault, to mute this hint, set AGENT_VAULT_VAULT to the vault you want to use.' >&2
		fi
	fi

	if [ -z "${AGENT_VAULT_TOKEN:-}" ]; then
		if ! AGENT_VAULT_TOKEN="$(
			agent-vault vault token \
				--address "$AGENT_VAULT_ADDR" \
				--vault "$AGENT_VAULT_VAULT"
		)"; then
			echo "ERR: Failed to retrieve vault token, set AGENT_VAULT_TOKEN first" >&2
			return 1
		fi
	fi

	if ! agent-vault ca verify; then
	    echo "ERR: Agent Vault CA is not installed in the system trust store" >&2
		echo "HINT: agent-vault: install the CA first: agent-vault ca install-script > agent-vault-ca-install.sh" >&2
		return 1
	fi

	agent_vault_proxy_url="http://${AGENT_VAULT_TOKEN}:${AGENT_VAULT_VAULT}@${AGENT_VAULT_MITM_ADDR}"

	export AGENT_VAULT_ADDR AGENT_VAULT_TOKEN AGENT_VAULT_VAULT
	export AGENT_VAULT_MITM_ADDR AGENT_VAULT_NO_PROXY
	export HTTPS_PROXY="$agent_vault_proxy_url"
	export https_proxy="$agent_vault_proxy_url"
	export HTTP_PROXY="$agent_vault_proxy_url"
	export http_proxy="$agent_vault_proxy_url"
	export NO_PROXY="$AGENT_VAULT_NO_PROXY"
	export no_proxy="$AGENT_VAULT_NO_PROXY"

	unset SSL_CERT_FILE CURL_CA_BUNDLE REQUESTS_CA_BUNDLE GIT_SSL_CAINFO DENO_CERT
	export UV_SYSTEM_CERTS=true
	export NODE_USE_ENV_PROXY=1
	export OPENCLAW_PROXY_URL="$agent_vault_proxy_url"
	if ! agent_vault_placeholder_script="$(agent-vault placeholders)"; then
		echo "ERR: Failed to get placeholders env" >&2
		unset agent_vault_placeholder_script agent_vault_proxy_url
		return 1
	fi
	if ! eval "$agent_vault_placeholder_script"; then
	    echo "ERR: Failed to evaluate placeholders env" >&2
		unset agent_vault_placeholder_script agent_vault_proxy_url
		return 1
	fi

	# Customize your environment here, for example:
	# export MISE_GITHUB_TOKEN="$GITHUB_TOKEN"

	unset agent_vault_placeholder_script agent_vault_proxy_url
}

agent_vault_env
agent_vault_env_status=$?
unset -f agent_vault_env
if [ "$agent_vault_env_status" -ne 0 ]; then
	# shellcheck disable=SC2317 # return is for sourcing; exit is for direct execution.
	return "$agent_vault_env_status" 2>/dev/null || exit "$agent_vault_env_status"
fi
unset agent_vault_env_status
