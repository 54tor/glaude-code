// Accès aux routes du serveur Go (gld-web/main.go).

export type Session = { id: string; dir: string; title: string; named: boolean; cwd: string; mtime: number; kb: number }
export type Group = { project: string; missing: boolean; sessions: Session[] }
export type VGroup = { name: string; project: string; ids: string[]; fav?: boolean }
export type Data = { home: string; root: string; groups: Group[]; live: string[]; vgroups: VGroup[]; favs: string[] }
export type Snippet = { path: string; kb: number; mtime: number }
export type MemFile = { key: string; name: string; kb: number; mtime: number; path: string }
export type MemProject = { dir: string; project: string; missing: boolean; files: MemFile[] }
export type TabSession = { id: string; dir: string; project: string; title: string }
export type TabSave = { name: string; auto: boolean; at: string; sessions: TabSession[] }
export type MemTarget = { dir: string; project: string }
export type Trashed = { id: string; key: string; path: string; reason: string; at: number; kb: number }
export type Hit = { dir: string; id: string; n: number; snip: string }
export type Item = { dir: string; id: string }
export type Detail = { prompts: string[]; n: number; branch: string; models: string[]; t0: string; t1: string; subagents: number }
export type Msg = { r: "user" | "assistant" | "tool"; t: string; ts?: string }
export type Ls = { path: string; parent: string; dirs: string[]; sep: string; error?: string }
export type Result = { ok: boolean; out: string }

const enc = encodeURIComponent

async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  return (await r.json()) as T
}

export const api = {
  data: () => get<Data>("/api"),
  info: (s: Item) => get<Detail>(`/info?dir=${enc(s.dir)}&id=${s.id}`),
  conv: (s: Item, tools: boolean) => get<{ msgs: Msg[] }>(`/conv?dir=${enc(s.dir)}&id=${s.id}${tools ? "&tools=1" : ""}`),
  ls: (path: string) => get<Ls>(`/ls?path=${enc(path)}`),
  snippets: () => get<{ dir: string; files: Snippet[] }>("/snippet-list"),
  snippet: (path: string) => get<{ text?: string; error?: string }>(`/snippet?path=${enc(path)}`),
  memory: () => get<{ global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }>("/memory-list"),
  memoryFile: (key: string) => get<{ text?: string; error?: string }>(`/memory-file?key=${enc(key)}`),
  tabs: () => get<{ saves: TabSave[] }>("/tabs"),
  trash: () => get<{ items: Trashed[] }>("/trash"),
  trashFile: (id: string) => get<{ text?: string; error?: string }>(`/trash-file?id=${enc(id)}`),
  search: (q: string) => get<{ hits: Hit[] }>(`/search?q=${enc(q)}`),
  // Une action ne lève jamais : toute erreur (réseau, HTTP) devient un Result visible.
  async post(url: string, body: unknown): Promise<Result> {
    try {
      const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
      return (await r.json()) as Result
    } catch (e) {
      return { ok: false, out: `Erreur : ${e instanceof Error ? e.message : String(e)}` }
    }
  },
}
