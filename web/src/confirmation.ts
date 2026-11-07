// Presentation policy only. Every write still uses server-prepared capabilities.
export function requiresConfirmation(
  mode: string | undefined,
  narrow = false,
): boolean {
  return mode === 'require' || (mode !== 'none' && !narrow);
}
