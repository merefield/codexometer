import type { Session } from './state.svelte';

// Share the exact visible-copy eligibility and untruncated payload with local commands.
export function sessionCopyText(session?: Session): string {
  if (
    !session ||
    !['LAST REPLY', 'LAST ACTIVITY'].includes(session.contextKind)
  )
    return '';
  const text =
    session.text ||
    (session.contextKind === 'LAST ACTIVITY'
      ? session.workingCommand || ''
      : '');
  return text.trim() ? text : '';
}
