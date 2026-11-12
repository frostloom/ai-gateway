/** An empty legacy scope means all models; malformed stored scopes never do. */
export function planModelScope(raw: string | undefined | null): { kind: 'all' | 'models' | 'invalid'; ids: string[] } {
  if (raw == null || raw === '') return { kind: 'all', ids: [] }
  try {
    const ids: unknown = JSON.parse(raw)
    if (!Array.isArray(ids) || ids.length > 100 || ids.some(id => typeof id !== 'string' || !id.trim() || id === '*' || [...id].length > 64 || id !== id.trim())) return { kind: 'invalid', ids: [] }
    if (new Set(ids).size !== ids.length) return { kind: 'invalid', ids: [] }
    return { kind: ids.length ? 'models' : 'all', ids }
  } catch { return { kind: 'invalid', ids: [] } }
}
