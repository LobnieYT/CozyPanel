import { ApiError } from "../api/client";

/**
 * The API's field errors of a failed mutation (`{ public_host: "…" }`), empty when it did
 * not fail or failed for a reason that is not about a field (a network error, a 500).
 * Replaces `save.error instanceof ApiError ? save.error.fields : {}` at every form.
 */
export function fieldErrors(error: unknown): Record<string, string> {
  return error instanceof ApiError ? error.fields : {};
}

/**
 * The first error of a form's section: the API keys nested failures
 * (`node_dns.servers`), so reading only the section's own key would show nothing.
 */
export function fieldPrefix(errors: Record<string, string>, prefix: string): string | undefined {
  if (errors[prefix]) return errors[prefix];
  const p = prefix + ".";
  for (const [k, v] of Object.entries(errors)) {
    if (k.startsWith(p)) return v;
  }
  return undefined;
}
