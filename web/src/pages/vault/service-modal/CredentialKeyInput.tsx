import Combobox from "../../../components/Combobox";

/**
 * Credential key input with suggestions from the vault's existing
 * credentials. Free text still works — a key that matches nothing is a
 * new credential to be created later from the Credentials tab.
 */
export default function CredentialKeyInput({
  value,
  onChange,
  credentialKeys,
  placeholder,
  error,
  compact,
  autoFocus,
}: {
  value: string;
  onChange: (next: string) => void;
  credentialKeys: string[];
  placeholder?: string;
  error?: boolean;
  compact?: boolean;
  autoFocus?: boolean;
}) {
  return (
    <Combobox
      value={value}
      onChange={onChange}
      onSelect={onChange}
      options={credentialKeys.map((k) => ({ id: k, label: k }))}
      placeholder={placeholder}
      error={error}
      compact={compact}
      autoFocus={autoFocus}
    />
  );
}
