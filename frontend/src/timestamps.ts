const dateFormatter = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
const clockFormatter = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
const exactFormatter = new Intl.DateTimeFormat(undefined, { dateStyle: 'long', timeStyle: 'long' });
const relativeFormatter = new Intl.RelativeTimeFormat('en', { numeric: 'always' });

function validDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatTime(value: string) {
  const date = validDate(value);
  return date ? clockFormatter.format(date) : '';
}

export function formatExactTimestamp(value: string) {
  const date = validDate(value);
  return date ? exactFormatter.format(date) : '';
}

function sameLocalDay(first: Date, second: Date) {
  return first.getFullYear() === second.getFullYear()
    && first.getMonth() === second.getMonth()
    && first.getDate() === second.getDate();
}

export function formatCreatedDate(value: string, now: number) {
  const date = validDate(value);
  if (!date) return '';
  const today = new Date(now);
  if (sameLocalDay(date, today)) return 'Today';
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (sameLocalDay(date, yesterday)) return 'Yesterday';
  return dateFormatter.format(date);
}

export function formatLastOpened(value: string, now: number): { label: string; time: string } {
  const date = validDate(value);
  if (!date) return { label: '', time: '' };
  const elapsed = now - date.getTime();
  // Future timestamps can occur after a clock adjustment; do not describe them as past activity.
  if (elapsed < 0 || elapsed >= 28 * 86400000) {
    return { label: dateFormatter.format(date), time: clockFormatter.format(date) };
  }
  const seconds = Math.floor(elapsed / 1000);
  let label: string;
  if (seconds < 5) label = 'Now';
  else if (seconds < 60) label = `${seconds}s ago`;
  else if (seconds < 3600) label = `${Math.floor(seconds / 60)} min ago`;
  else if (seconds < 86400) label = relativeFormatter.format(-Math.floor(seconds / 3600), 'hour');
  else if (seconds < 604800) label = relativeFormatter.format(-Math.floor(seconds / 86400), 'day');
  else label = relativeFormatter.format(-Math.floor(seconds / 604800), 'week');
  return { label, time: '' };
}
