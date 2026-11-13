export const itemKey = item => JSON.stringify([item.source, item.path]);
export const isFolder = item => item.type === 'directory' || item.isDir === true;

export function organizerFilterCount(query, category, filters = {}) {
  return [query, category, filters.minSize, filters.maxSize, filters.modifiedAfter, filters.modifiedBefore]
    .filter(value => String(value ?? '').trim() !== '').length;
}

export function duplicateReviewSelection(group, selectedKeeperKey) {
  const files = Array.isArray(group?.files) ? group.files : [];
  if (!files.length || typeof group?.sha256 !== 'string' || !group.sha256) {
    return { keeper: null, copies: [], snapshots: [] };
  }

  const keeper = files.find(file => itemKey(file) === selectedKeeperKey) || files[0];
  const copies = files.filter(file => itemKey(file) !== itemKey(keeper));
  const keeperIdentity = { source: keeper.source, path: keeper.path };
  const snapshots = copies.map(file => ({
    key: itemKey(file),
    sha256: group.sha256,
    keeper: keeperIdentity,
  }));

  return { keeper, copies, snapshots };
}

export function normalizePath(path) {
  const parts = [];
  for (const part of String(path || '/').split('/')) {
    if (!part || part === '.') continue;
    if (part === '..') parts.pop();
    else parts.push(part);
  }
  return `/${parts.join('/')}${parts.length ? '/' : ''}`;
}

export function virtualWindow(total, scrollTop, viewport, rowHeight, overscan = 6) {
  scrollTop = Math.max(0, scrollTop);
  const start = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const end = Math.min(total, Math.ceil((scrollTop + viewport) / rowHeight) + overscan);
  return { start: Math.min(start, total), end, before: Math.min(start, total) * rowHeight, after: Math.max(0, total - end) * rowHeight };
}

export function selectRange(items, selected, anchor, index, { shift = false, toggle = false } = {}) {
  const next = toggle ? new Set(selected) : new Set();
  if (shift && anchor >= 0) {
    for (let n = Math.min(anchor, index); n <= Math.max(anchor, index); n++) {
      if (items[n]) next.add(itemKey(items[n]));
    }
  } else if (items[index]) {
    const key = itemKey(items[index]);
    if (toggle && next.has(key)) next.delete(key);
    else next.add(key);
  }
  return next;
}

export function visibleItems(items, query, category, sort, direction, filters = {}) {
  const needle = query.trim().toLocaleLowerCase();
  const sizeInBytes = value => value === '' || value === undefined || !Number.isFinite(Number(value)) || Number(value) < 0 ? null : Number(value) * 1024 * 1024;
  const dateStart = value => {
    if (!value) return null;
    const [year, month, day] = value.split('-').map(Number);
    const timestamp = new Date(year, month - 1, day).getTime();
    return Number.isFinite(timestamp) ? timestamp : null;
  };
  const minSize = sizeInBytes(filters.minSize);
  const maxSize = sizeInBytes(filters.maxSize);
  const after = dateStart(filters.modifiedAfter);
  const before = dateStart(filters.modifiedBefore);
  const beforeExclusive = before === null ? null : new Date(before).setDate(new Date(before).getDate() + 1);
  const filtered = items.filter(item => (!needle || item.name.toLocaleLowerCase().includes(needle)) &&
    (!category || (category === 'directory' ? isFolder(item) : !isFolder(item) && (item.type || '').startsWith(category))) &&
    (isFolder(item) || (minSize === null || Number(item.size) >= minSize)) &&
    (isFolder(item) || (maxSize === null || Number(item.size) <= maxSize)) &&
    (isFolder(item) || after === null || (Number.isFinite(Date.parse(item.modified)) && Date.parse(item.modified) >= after)) &&
    (isFolder(item) || beforeExclusive === null || (Number.isFinite(Date.parse(item.modified)) && Date.parse(item.modified) < beforeExclusive)));
  const value = item => sort === 'extension' ? item.name.split('.').slice(1).pop() || '' : item[sort];
  return filtered.sort((a, b) => {
    if (isFolder(a) !== isFolder(b)) return isFolder(a) ? -1 : 1;
    const av = value(a), bv = value(b);
    if (av === undefined || av === null || av === '') return bv === undefined || bv === null || bv === '' ? 0 : 1;
    if (bv === undefined || bv === null || bv === '') return -1;
    const comparison = sort === 'size' ? Number(av) - Number(bv) : sort === 'modified' ? Date.parse(av) - Date.parse(bv) : String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: 'base' });
    return comparison * (direction === 'desc' ? -1 : 1);
  });
}

export function favoritesKey(user) {
  const identity = user?.id ?? user?.username;
  return identity === undefined || identity === null || identity === '' ? null : `organizer:favorites:${encodeURIComponent(String(identity))}`;
}

export function formatBytes(bytes) {
  if (!Number.isFinite(Number(bytes))) return '—';
  const n = Number(bytes);
  if (n === 0) return '0 B';
  const unit = Math.min(4, Math.max(0, Math.floor(Math.log(Math.abs(n)) / Math.log(1024))));
  return `${(n / 1024 ** unit).toLocaleString(undefined, { maximumFractionDigits: unit ? 1 : 0 })} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`;
}

/** Read only a bounded prefix even if an older server ignores the Range header. */
export async function readPreviewText(response, maxBytes = 65536) {
  if (!response.body?.getReader) {
    const size = Number(response.headers.get('content-length'));
    if (!size || size > maxBytes) throw new Error('A bounded text preview is unavailable. Open the full viewer.');
    return { text: await response.text(), truncated: false };
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let text = '', received = 0, truncated = false;
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      const remaining = maxBytes - received;
      text += decoder.decode(chunk.value.subarray(0, remaining), { stream: true });
      received += Math.min(chunk.value.length, remaining);
      if (received >= maxBytes) { truncated = true; break; }
    }
    if (!truncated) text += decoder.decode();
  } finally {
    await reader.cancel();
    reader.releaseLock();
  }
  const contentRange = response.headers.get('content-range');
  if (contentRange) { const match = /\/(\d+)$/.exec(contentRange); if (match && Number(match[1]) > received) truncated = true; }
  return { text, truncated };
}
