import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../src/theme.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { readTheme, applyTheme } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('Isolate preserves legacy theme preferences and saves under its new name', () => {
  const values = new Map([['sessionsplit-theme', 'dark']]);
  globalThis.localStorage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
  globalThis.document = {
    documentElement: { dataset: {} },
    querySelector: () => null,
  };
  globalThis.window = { matchMedia: () => ({ matches: false }) };
  try {
    assert.equal(readTheme(), 'dark');
    applyTheme('light', true);
    assert.equal(values.get('isolate-theme'), 'light');
    assert.equal(values.get('sessionsplit-theme'), 'dark');
    assert.equal(readTheme(), 'light');
    values.clear();
    assert.equal(readTheme(), 'light');
  } finally {
    delete globalThis.localStorage;
    delete globalThis.document;
    delete globalThis.window;
  }
});
