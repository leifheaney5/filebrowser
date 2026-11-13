import { describe, it, expect } from 'vitest';
import { favoritesKey, itemKey, normalizePath, readPreviewText, selectRange, virtualWindow, visibleItems } from './organizer';

describe('organizing workspace', () => {
  const items = Array.from({ length: 8 }, (_, n) => ({ name: `file ${n}`, source: 'disk', path: `/${n}`, type: 'text/plain' }));
  it('bounds rendered rows for a hundred thousand entries', () => {
    const window = virtualWindow(100000, 500000, 640, 48);
    expect(window.end - window.start).toBeLessThan(28);
    expect(window.before + window.after + (window.end - window.start) * 48).toBe(4800000);
    expect(virtualWindow(50, -1000, 640, 48).start).toBe(0);
    expect(virtualWindow(50, -1000, 640, 48).end).toBeGreaterThan(0);
  });
  it('keeps shift ranges inclusive and supports toggle deselection', () => {
    const selected = selectRange(items, new Set(), 2, 5, { shift: true });
    expect([...selected]).toEqual(items.slice(2, 6).map(itemKey));
    expect(selectRange(items, selected, 2, 3, { toggle: true }).has(itemKey(items[3]))).toBe(false);
  });
  it('cannot share favorite storage between accounts or anonymous sessions', () => {
    expect(favoritesKey({})).toBe(null);
    expect(favoritesKey({ username: 'one' })).not.toBe(favoritesKey({ username: 'two' }));
  });
  it('normalizes editable paths without traversing above root', () => {
    expect(normalizePath('/one/../two//')).toBe('/two/');
    expect(normalizePath('/../../')).toBe('/');
  });
  it('filters file types and sorts numeric sizes with directories first', () => {
    const files = [{ name: 'b', type: 'directory' }, { name: 'a', size: 10, type: 'image/jpeg' }, { name: 'c', size: 2, type: 'image/png' }];
    expect(visibleItems(files, '', '', 'size', 'asc').map(i => i.name)).toEqual(['b', 'c', 'a']);
    expect(visibleItems(files, '', 'image', 'size', 'asc').map(i => i.name)).toEqual(['c', 'a']);
  });
  it('cancels a text response once the preview byte budget is reached', async () => {
    let cancelled = false;
    const response = new Response(new ReadableStream({
      start(controller) { controller.enqueue(new TextEncoder().encode('abcdefghij')); },
      cancel() { cancelled = true; },
    }));
    const preview = await readPreviewText(response, 4);
    expect(preview).toEqual({ text: 'abcd', truncated: true });
    expect(cancelled).toBe(true);
  });
  it('recognizes a truncated Range response and preserves unicode', async () => {
    const response = new Response('café', { headers: { 'content-range': 'bytes 0-4/100' } });
    expect(await readPreviewText(response, 10)).toEqual({ text: 'café', truncated: true });
    expect(await readPreviewText(new Response('café'), 4)).toEqual({ text: 'caf', truncated: true });
  });
  it('sorts timestamps by their instant across timezone offsets', () => {
    const files = [{ name: 'later', modified: '2026-11-13T10:00:00-05:00' }, { name: 'earlier', modified: '2026-11-13T12:00:00Z' }];
    expect(visibleItems(files, '', '', 'modified', 'asc').map(file => file.name)).toEqual(['earlier','later']);
  });
});
