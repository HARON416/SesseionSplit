import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// Exercise local calendar comparisons across daylight-saving transitions.
process.env.TZ = 'America/New_York';
const source = await readFile(new URL('../src/timestamps.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { formatCreatedDate, formatExactTimestamp, formatLastOpened, formatTime } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
const now = new Date('2026-11-07T12:00:00-05:00').getTime();

test('relative labels change at the agreed thresholds and pluralize correctly', () => {
  for (const [seconds, expected] of [
    [0, 'Now'], [4.999, 'Now'], [5, '5s ago'], [59, '59s ago'],
    [60, '1 min ago'], [3599, '59 min ago'], [3600, '1 hour ago'],
    [7200, '2 hours ago'], [86399, '23 hours ago'], [86400, '1 day ago'],
    [172800, '2 days ago'], [604799, '6 days ago'], [604800, '1 week ago'],
    [1209600, '2 weeks ago'], [2419199, '3 weeks ago'],
  ]) {
    assert.deepEqual(formatLastOpened(new Date(now - seconds * 1000).toISOString(), now), { label: expected, time: '' });
  }
});

test('old and future activity use a full date and time', () => {
  for (const offset of [-28 * 86400000, 60000]) {
    const value = new Date(now + offset).toISOString();
    const result = formatLastOpened(value, now);
    assert.equal(result.label, new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(value)));
    assert.equal(result.time, formatTime(value));
    assert.ok(result.time);
  }
});

test('Created uses local calendar days including midnight, month/year boundaries and DST', () => {
  for (const [current, value, expected] of [
    ['2026-11-07T12:00:00-05:00', '2026-11-07T00:00:00-05:00', 'Today'],
    ['2026-11-07T00:01:00-05:00', '2026-11-06T23:59:00-05:00', 'Yesterday'],
    ['2026-01-01T00:01:00-05:00', '2025-12-31T23:59:00-05:00', 'Yesterday'],
    ['2026-03-01T12:00:00-05:00', '2026-02-28T12:00:00-05:00', 'Yesterday'],
    ['2026-03-08T23:30:00-04:00', '2026-03-07T23:45:00-05:00', 'Yesterday'],
    ['2026-11-01T23:30:00-05:00', '2026-10-31T00:01:00-04:00', 'Yesterday'],
  ]) assert.equal(formatCreatedDate(value, new Date(current).getTime()), expected);
  assert.notEqual(formatCreatedDate('2026-11-05T12:00:00-05:00', now), 'Yesterday');
});

test('invalid or missing timestamps remain unavailable, exact labels retain date/time', () => {
  for (const value of ['', 'invalid']) {
    assert.equal(formatCreatedDate(value, now), '');
    assert.deepEqual(formatLastOpened(value, now), { label: '', time: '' });
    assert.equal(formatExactTimestamp(value), '');
    assert.equal(formatTime(value), '');
  }
  const exact = formatExactTimestamp(new Date(now).toISOString());
  assert.match(exact, /2026/);
  assert.match(exact, /12:00:00/);
});
