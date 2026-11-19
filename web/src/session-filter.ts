export function matchesSession(
  query: string,
  row: { id: string; name?: string; directory: string },
): boolean {
  const term = query.trim().toLowerCase();
  return (
    !term ||
    [row.id, row.name, row.directory].some((v) =>
      v?.toLowerCase().includes(term),
    )
  );
}
