import { i18n } from "@/i18n"
import { readResponse } from "@/lib/apiError"
import { authFetch } from "@/lib/auth"
import { http, qs, seg, v1 } from "@/lib/http"

// Accès aux routes du serveur Go (gld/main.go).

// Fournisseurs d'agents de code (provider.go) : ce que chacun sait faire ; l'interface masque le reste.
export type Caps = { hotCache: boolean; keepWarm: boolean; config: boolean; memory: boolean; tuiPrompts: boolean; resume: boolean }
export type ProviderInfo = { id: string; name: string; caps: Caps }
export type Session = { provider?: string; id: string; dir: string; title: string; named: boolean; cwd: string; mtime: number; kb: number; tok: Tokens; parent?: string }
export type Tokens = { in: number; out: number; cr: number; cw: number }
export type Group = { project: string; missing: boolean; sessions: Session[] }
export type Project = { name: string; dirs: string[] } // ensemble nommé de dossiers de travail
export type VGroup = { name: string; project: string; ids: string[]; fav?: boolean }
export type Data = { home: string; root: string; groups: Group[]; dirs: string[]; live: string[]; background?: string[]; waiting: string[]; asking?: string[]; vgroups: VGroup[]; favs: string[]; projects: Project[]; providers: ProviderInfo[]; lang?: string }
// Snippet = commande Claude Code. key : u/<chemin> (personnelle) ou p/<dossier>/<chemin> (d'un projet).
export type Snippet = { key: string; scope: string; project: string; path: string; command: string; description: string; kb: number; mtime: number }
export type SnipScope = { dir: string; project: string }
export type MemFile = {
  key: string
  name: string
  kb: number
  mtime: number
  path: string
  type?: string // metadata.type du front matter
  paths?: string[] // règle scopée : globs du champ paths:
  tokens: number // estimation calibrée, ou compte réel si exact
  exact?: boolean // tokens est un compte réel (Haiku)
  always?: boolean // chargé à chaque session
  off?: boolean // règle désactivée (.md.off)
  warn?: string // front matter invalide
}
export type LoadEvent = { at: number; session: string; file: string; type?: string; reason?: string; globs?: string[]; trigger?: string }
export type FileLoads = { sessions: number; last: number }
// Skills (skills.go)
export type PluginInst = { id: string; version: string; scope: string; enabled: boolean; project?: string }
export type PluginAvail = { id: string; name: string; description: string; marketplace: string; installs: number; installed: boolean }
export type Marketplace = { name: string; source: string; repo?: string; url?: string }
export type Skill = {
  key: string
  name: string
  dir: string
  scope: "user" | "project" | "plugin" | "store"
  origin: "store" | "plugin" | "install" | "custom" // d'où vient le skill
  source?: string
  project?: string
  plugin?: string
  readOnly?: boolean
  description: string
  chars: number
  descTokens: number
  tokens: number
  manual?: boolean
  hidden?: boolean
  tools?: string
  files: number
  kb: number
  mtime: number
  path: string
  warn?: string
  lock?: SkillLock // posé par npx skills
}
export type SkillLock = { source: string; sourceType: string; skillPath: string; hash: string; installedAt: string; updatedAt: string; link: boolean; global: boolean; managed: boolean }
export type SkillHit = { ref: string; source: string; skill: string; installs: string; url: string }
export type SkillFile = { rel: string; kb: number }

// Ce que chaque projet configure en propre (configproj.go)
export type ProjectCfg = {
  dir: string
  project: string
  files: { id: string; label: string; rows: { path: string; value: string }[] }[]
  perms: Record<string, { rule: string; scope: string }[]>
  hooks: HookInfo[]
  mcp: McpServer[]
  rules: { name: string; paths?: string[]; warn?: string }[]
  skills: string[]
  docs: string[]
  specific: number
}

// Configuration de Claude Code (config*.go)
export type ConfigFile = { id: string; scope: "user" | "project" | "local"; dir?: string; label: string; path: string; exists: boolean; error?: string; data?: unknown }
export type HookInfo = { scope: string; file: string; event: string; matcher?: string; type: string; command: string; missing?: string }
export type CfgIssue = { file: string; kind: string; msg: string }
export type EffRow = { path: string; values: Record<string, string>; winner: string; merged?: boolean }
export type McpServer = { name: string; scope: string; project?: string; type: string; command?: string; url?: string; envKeys?: string[] }
export type PermSuggest = { rule: string; count: number; readOnly: boolean; example: string }
export type BackupInfo = { name: string; at: string; file: string; kb: number }
export type CfgOverview = {
  files: ConfigFile[]
  hooks: HookInfo[]
  issues: CfgIssue[]
  backups: BackupInfo[]
  plugins: { id: string; enabled: boolean; installed: boolean }[]
  projects: { dir: string; project: string }[]
}
export type CfgEdit = { file: string; op: "set" | "delete" | "add" | "remove"; path: string[]; value?: unknown }
export type Finding = { kind: string; keys: string[]; why: string }
export type Issue = { key: string; kind: string; msg: string; dir?: string }
export type MemProject = { dir: string; project: string; missing: boolean; files: MemFile[] }
export type TabSession = { id: string; dir: string; project: string; title: string }
export type TabSave = { name: string; auto: boolean; at: string; sessions: TabSession[] }
export type MemTarget = { dir: string; project: string }
export type Trashed = { id: string; key: string; path: string; reason: string; at: number; kb: number }
export type Hit = { dir: string; id: string; n: number; snip: string }
export type Item = { dir: string; id: string }
export type Detail = { prompts: string[]; n: number; branch: string; models: string[]; t0: string; t1: string; subagents: number }
export type SubLive = { id: string; desc: string; type: string; running: boolean; activity: string; context: number; out: number; mtime: number }
export type WfAgent = { label: string; phase: string; state: string; model: string; tokens: number; toolCalls: number; durationMs: number; lastTool: string }
export type Workflow = { runId: string; taskId: string; name: string; summary: string; status: string; startTime: number; durationMs: number; totalTokens: number; totalToolCalls: number; phases: { title: string; detail: string }[]; agents: WfAgent[]; logs: string[] }
export type BgTask = { id: string; tool?: string; desc: string; status: string; summary?: string; start?: string; end?: string; hasOutput: boolean }
export type LiveState = { said?: string; open: boolean; status: string; activity: string; thinking: boolean; generic?: boolean; exec?: boolean; since: number; context: number; out: number; model: string; tool?: string; toolRunning?: boolean; subs: SubLive[]; tasks?: BgTask[]; verbs?: string[]; queue?: { t: string; ts: string }[]; pending?: QItem[] }
// File de messages côté serveur (queue.go) : auteur, et état d'envoi (uncertain : parti sans réponse, à trancher).
export type QItem = { id: string; text: string; author: "host" | "guest"; guest?: string; label?: string; state: "queued" | "sending" | "uncertain"; at: number; sentAt?: number }
export type UsageLimit = { key: string; label: string; percent: number; resetsAt?: string; limitDollars?: number; usedDollars?: number; lockedWhy?: string }
export type Account = {
  email: string
  name?: string
  org?: string
  orgType?: string
  tier?: string
  billing?: string
  extraUsage: boolean
  extraWhy?: string
  since?: string
  limits: UsageLimit[]
  limitsAt?: number
  refreshing?: boolean
  totalSessions: number
  totalMessages: number
  firstSession?: string
  days: { date: string; messages: number; sessions: number; tools: number; tokens?: Record<string, number> }[]
  statsAt?: string
  models: { model: string; in: number; out: number; cacheRead: number; cacheWrite: number }[]
}
// cut/len : texte coupé à l'octet cut d'un texte de len octets (message démesuré) ; la suite vient de api.convText.
export type Msg = { r: "user" | "assistant" | "tool" | "doc"; t: string; ts?: string; o?: number; n?: number; img?: string; k?: number; cut?: number; len?: number }
export type Ls = { path: string; parent: string; dirs: string[]; sep: string; error?: string }
// Réponse textuelle d'une action de l'API /v1 (toast).
export type Said = { message: string }
export type SentNote = { id: string; at: number; text: string; to: string }
// Le serveur plafonne une requête multiple à 100 éléments ; les ?item= restent sous ~6 Ko d'URL pour les proxys.
export const BULK_MAX = 100
const BULK_URL = 6000
export function batches<T>(list: T[], size: (t: T) => number = () => 0): T[][] {
  const out: T[][] = []
  let cur: T[] = []
  let n = 0
  for (const x of list) {
    const s = size(x)
    if (cur.length && (cur.length >= BULK_MAX || n + s > BULK_URL)) {
      out.push(cur)
      cur = []
      n = 0
    }
    cur.push(x)
    n += s
  }
  if (cur.length) out.push(cur)
  return out
}
const itemLen = (v: string) => encodeURIComponent(v).length + 6
// Lots à la suite ; les compteurs et les listes des réponses s'additionnent, et un lot en échec arrête la suite.
export async function inBatches<T, R extends { message?: string }>(list: T[], run: (b: T[]) => Promise<R>, size?: (t: T) => number): Promise<R> {
  const bs = batches(list, size)
  if (bs.length <= 1) return run(bs[0] ?? [])
  const rs: R[] = []
  for (const b of bs) rs.push(await run(b))
  const out: Record<string, unknown> = {}
  for (const r of rs)
    for (const [k, v] of Object.entries(r)) {
      if (typeof v === "number") out[k] = ((out[k] as number) ?? 0) + v
      else if (Array.isArray(v)) out[k] = [...((out[k] as unknown[]) ?? []), ...v]
      else if (k !== "message") out[k] = v
    }
  const lines = rs.map((r) => (r.message ?? "").split("\n")[0]).filter(Boolean)
  out.message = [i18n.t("shell.act.batches", { n: rs.length }), ...lines].join("\n")
  return out as R
}
export type Bulk = { done: string[]; failed: { item: string; error: { code: string; message: string } }[] }
export type CfgReply = { out: string; before: string; after: string; warnings?: CfgIssue[] }
// État git d'un dossier (gitstatus.go)
export type GitState = { branch: string; detached?: boolean; upstream: boolean; ahead: number; behind: number; changed: number; untracked: number }
// Maintien au chaud d'une session (keepwarm.go) : pings automatiques, comptés et plafonnés.
export type WarmState = { dir: string; on: boolean; pings: number; max: number; lastPing: number; note?: string }
export type GitRemote = { name: string; url: string }
export type GitChange = { path: string; status: string; from?: string } // status : « XY » comme git status --short
// Dossiers de configuration Claude (homes.go) : current "" = celui du processus principal (main).
// main "" et mainId : mode multi-utilisateur, le principal sans droit dessus ; id : clé des droits, connue du serveur.
// Appareil jumelé par QR code en mode jeton (pair.go) ; seen : dernière visite, en secondes Unix.
export type Device = { id: string; name: string; ip: string; created: number; seen: number }
export type BackupPlan = { id: string; manifest: { format: number; gld: string; date: string; cats: string[] }; items: SyncItem[]; targets: MemTarget[]; skipped: string[] }
export type BackupResult = { id: string; status: "ok" | "failed" | "skipped"; message?: string }
export type SyncItem = { id: string; kind: string; label: string; status: "new" | "diff" | "same"; risk?: string; merge: boolean; cat?: string; from: string; to: string }
export type HomesInfo = { current: string; main: string; mainId?: string; homes: { name: string; dir: string; id?: string; running: boolean }[] }

// status : 403 dit un droit manquant (mode multi-utilisateur), à distinguer d'un serveur injoignable.
export type InviteQueued = { id: string; text: string; state: "queued" | "sending" | "uncertain"; at: number }
export type InviteInfo = { id: string; seq: number; name: string; label: string; mode: "rw" | "ro"; state: "connected" | "away" | "pending"; lastSeen: number; redeemed: boolean; queue: InviteQueued[] }
export type ShareInfo = { id: string; dir: string; title: string; handle: string; open: boolean; invites: InviteInfo[] }
export type GuestShare = { handle: string; title: string; mode: "rw" | "ro" }

export type FsTrashItem = { id: string; path: string; mtime: number }
export type FsEntry = { name: string; type: "dir" | "file" | "symlink"; size: number; mtime: number }
export type FsFile = { content: string; etag: string; size: number; mtime: number }

export const api = {
  fsTree: (dir: string, path: string) => http.get<{ entries: FsEntry[]; truncated: boolean }>(`/fs/tree${qs({ dir, path })}`),
  fsFile: (dir: string, path: string) => http.get<FsFile>(`/fs/entries/content${qs({ dir, path })}`),
  fsSave: (dir: string, path: string, content: string, etag: string, force?: boolean) => http.put<{ etag: string }>(`/fs/entries/content${qs({ dir, path })}`, { content, etag, force }),
  fsNew: (dir: string, path: string, kind: "file" | "dir") => http.post(`/fs/entries${qs({ dir })}`, { path, kind }),
  fsRename: (dir: string, from: string, to: string) => http.patch(`/fs/entries${qs({ dir })}`, { from, to }),
  fsRm: (dir: string, path: string) => http.del<Bulk>(`/fs/entries${qs({ dir, item: path })}`),
  fsTrash: (dir: string) => http.get<{ items: FsTrashItem[] }>(`/fs/trash${qs({ dir })}`),
  fsTrashRestore: (dir: string, id: string) => http.patch(`/fs/trash/${seg(id)}${qs({ dir })}`, { trashed: false }),
  fsTrashPurge: (dir: string, id: string) => http.del<Bulk>(`/fs/trash${qs({ dir, item: id })}`),
  fsIndex: (dir: string) => http.get<{ state: string; files: { path: string; size: number; mtime: number }[] }>(`/fs/index${qs({ dir })}`),
  fsFiles: (dir: string, paths: string[]) => http.post<{ files: { path: string; content: string }[] }>(`/fs/files${qs({ dir })}`, { paths }),
  data: () => http.get<Data>("/state"),
  banAlerts: (after: number) =>
    http.get<{ cursor: number; bans: { at: number; ip: string }[]; pairings: { at: number; ip: string }[] }>(`/auth/alerts${qs({ after })}`),
  cleanupWarning: () => http.get<{ days: number; sessions: { dir: string; id: string; title: string; cwd: string; expires: number }[] }>("/cleanup-warning"),
  sessionsStamp: () => http.get<{ stamp: number }>("/state/stamp"),
  sessionTrash: () => http.get<{ sessions: { dir: string; id: string; title: string; cwd: string; at: number; kb: number }[]; days: number }>("/trash/sessions"),
  info: (s: Item) => http.get<Detail>(`/info${qs({ dir: s.dir, id: s.id })}`),
  // Lecture par la fin, `before` = « start » rendu par la page précédente. Avec `limit` : autant de messages, textes coupés à
  // 64 Kio (le lecteur). Sans `limit` : pages de 1000 messages aux textes entiers (l'export).
  conv: (s: Item & { sub?: string }, tools: boolean, page?: { limit?: number; before?: number; anchors?: boolean }) =>
    http.get<{ msgs: Msg[]; start?: number; more?: boolean }>(
      `/sessions/${seg(s.id)}/conversation${qs({ dir: s.dir, sub: s.sub, tools, limit: page?.limit || undefined, before: page ? (page.before ?? 0) : undefined, anchors: !!page?.anchors && !tools })}`
    ),
  // Messages d'outil des lignes [from, until) du transcript, pour déplier une ancre.
  convTools: (s: Item, from: number, until: number) => http.get<{ msgs: Msg[] }>(`/sessions/${seg(s.id)}/conversation/tools${qs({ dir: s.dir, from, until })}`),
  // Suite d'un message coupé, à partir de l'octet from ; cut = 0 : fin du texte.
  convText: (s: Item, tools: boolean, m: Msg, from: number) =>
    http.get<{ t: string; cut: number }>(`/sessions/${seg(s.id)}/conversation/text${qs({ dir: s.dir, o: m.o ?? 0, n: m.n ?? 0, from, tools })}`),
  ls: (path: string) => http.get<Ls>(`/directories${qs({ path })}`),
  homes: () => http.get<HomesInfo>("/homes"),
  devices: () => http.get<{ devices: Device[] | null }>("/devices"),
  keepWarm: () => http.get<{ items: Record<string, WarmState>; ttl: number; below: number }>("/keepwarm"),
  gitChanges: (dir: string) => http.get<{ files: GitChange[]; more?: number; prefix?: string }>(`/git/changes${qs({ dir })}`),
  notes: () => http.get<{ notes: Record<string, string> }>("/notes"),
  notesSent: (key: string) => http.get<{ sent: SentNote[] }>(`/notes/sent${qs({ key })}`),
  trustPending: () => http.get<{ dirs: string[] }>("/trust/pending"),
  gitRemotes: (dir: string) => http.get<{ remotes: GitRemote[] }>(`/git/remotes${qs({ dir })}`),
  gitStatus: () => http.get<{ status: Record<string, GitState>; repos: Record<string, string[]> }>("/git/status"),
  snippets: () => http.get<{ dir: string; files: Snippet[]; scopes: SnipScope[] }>("/snippets"),
  snippet: (key: string) => http.get<{ text: string; missing?: boolean }>(`/snippets/${seg(key)}`),
  snippetSave: (key: string, text: string, create: boolean) => http.put<Said>(`/snippets/${seg(key)}`, { text, create }),
  snippetRename: (from: string, to: string) => http.patch<Said>(`/snippets/${seg(from)}`, { to }),
  snippetRemove: (keys: string[]) => inBatches(keys, (b) => http.del<Said & Bulk>(`/snippets${qs({ item: b })}`), itemLen),
  snippetMerge: (body: { keys: string[]; out?: string; launch: boolean; prompt?: string; args?: string }) => http.post<Said & { prompt: string }>("/snippets/merges", body),
  snippetDraft: (body: { model: string; instruction: string; text: string }) => http.post<Said & { text: string; cost?: number }>("/snippets/drafts", body),
  memory: () => http.get<{ global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }>("/memory/files"),
  memoryFile: (key: string) => http.get<{ text: string; missing?: boolean }>(`/memory/files/${seg(key)}`),
  memoryLoaded: () => http.get<{ hook: boolean; files: Record<string, FileLoads> }>("/memory/loaded"),
  memorySave: (key: string, text: string, index = false) => http.put<Said>(`/memory/files/${seg(key)}`, { text, index }),
  memoryRename: (key: string, name: string) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { name }),
  memoryMove: (key: string, to: string, copy = false) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { to, copy }),
  memoryToggle: (key: string, enabled: boolean) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { enabled }),
  memoryType: (key: string, type: string) => http.patch<Said>(`/memory/files/${seg(key)}`, { type }),
  memoryPaths: (key: string, paths: string[]) => http.patch<Said>(`/memory/files/${seg(key)}`, { paths }),
  memoryRemove: (keys: string[]) => inBatches(keys, (b) => http.del<Said & Bulk>(`/memory/files${qs({ item: b })}`), itemLen),
  memoryFix: (key: string) => http.post<Said>("/memory/fixes", { key }),
  memoryRepair: (dir: string) => http.post<Said>("/memory/repairs", { dir }),
  memoryDraft: (body: { model: string; instruction: string; key: string; refs: string[] }) => http.post<Said & { text: string; cost?: number }>("/memory/drafts", body),
  memoryMerge: (body: { keys: string[]; out?: string; launch: boolean; prompt?: string; args?: string }) => http.post<Said & { prompt: string }>("/memory/merges", body),
  memoryTokens: (keys: string[]) => http.post<{ counts: Record<string, number>; errors: Record<string, string>; cost: number }>("/memory/token-counts", { keys }),
  memoryLint: (scope: string) => http.post<{ findings: Finding[]; cost?: number }>("/memory/lints", { scope }),
  sessionLoaded: (id: string) => http.get<{ files: LoadEvent[] }>(`/sessions/${seg(id)}/loaded`),
  sessionLoadedFile: (id: string, file: string) => http.get<{ text?: string }>(`/sessions/${seg(id)}/loaded/files/${seg(file)}`),
  configRaw: (file: string) => http.get<{ text: string }>(`/config/raw${qs({ file })}`),
  slashCommands: () => http.get<{ commands: { cmd: string; desc: string; origin: string }[] }>("/slash-commands"),
  skills: () => http.get<{ skills: Skill[]; projects: { dir: string; project: string }[] }>("/skills"),
  skill: (key: string) => http.get<{ text: string; files?: SkillFile[] }>(`/skills/${seg(key)}`),
  skillSave: (key: string, text: string, create: boolean) => http.put<Said>(`/skills/${seg(key)}`, { text, create }),
  skillRemove: (keys: string[]) => inBatches(keys, (b) => http.del<Said & Bulk>(`/skills${qs({ item: b })}`), itemLen),
  skillCopy: (key: string, to: string) => http.post<Said & { key: string }>("/skills/copies", { key, to }),
  skillDraft: (body: { model: string; instruction: string; key: string }) => http.post<Said & { text: string; cost?: number }>("/skills/drafts", body),
  skillNpx: (body: { Action: string; Source: string; Skill: string; Scope: string; Project: string }) => http.post<Said>("/skills/installations", body),
  skillsFind: (q: string) => http.get<{ hits?: SkillHit[] }>(`/skills/catalog${qs({ q })}`),
  skillsUpdates: (force = false) => http.get<{ states?: Record<string, string> }>(`/skills/updates${qs({ force })}`),
  marketplaces: () => http.get<{ marketplaces?: Marketplace[] }>("/marketplaces"),
  plugins: (q = "") => http.get<{ installed?: PluginInst[]; available?: PluginAvail[] }>(`/plugins${qs({ q })}`),
  pluginAct: (action: "install" | "uninstall" | "enable" | "disable" | "update", id: string, scope: string, project: string) => {
    const p = `/plugins/${seg(id)}`
    switch (action) {
      case "install":
        return http.put<Said>(p, { scope, project })
      case "uninstall":
        return http.del<Said>(`${p}${qs({ scope, project })}`)
      case "update":
        return http.post<Said>(`${p}/updates`, { scope, project })
      default:
        return http.patch<Said>(p, { enabled: action === "enable", scope, project })
    }
  },
  marketAct: (action: "add" | "remove" | "update", source: string, name: string) =>
    action === "add" ? http.post<Said>("/marketplaces", { source }) : action === "remove" ? http.del<Said>(`/marketplaces/${seg(name)}`) : http.post<Said>(name ? `/marketplaces/${seg(name)}/updates` : "/marketplaces/updates"),
  configProjects: () => http.get<{ projects: ProjectCfg[] }>("/config/projects"),
  sessionIdle: (id: string) => http.get<{ idle: boolean }>(`/sessions/${seg(id)}/state`),
  workflows: (dir: string, id: string) => http.get<{ workflows: Workflow[] }>(`/sessions/${seg(id)}/workflows${qs({ dir })}`),
  taskOutput: (dir: string, id: string, task: string) => http.get<{ text?: string }>(`/sessions/${seg(id)}/tasks/${seg(task)}/output${qs({ dir })}`),
  queue: (id: string) => http.get<{ items: QItem[] }>(`/sessions/${seg(id)}/queue`),
  queueAdd: (id: string, text: string) => http.post<{ item: QItem }>(`/sessions/${seg(id)}/queue`, { text }),
  queueRm: (id: string, item: string) => http.del<{ text: string }>(`/sessions/${seg(id)}/queue/${seg(item)}`),
  queueRetry: (id: string, item: string) => http.patch(`/sessions/${seg(id)}/queue/${seg(item)}`, { state: "queued" }),
  convRewind: (dir: string, id: string, o: number) => http.del<{ n: number; message: string }>(`/sessions/${seg(id)}/conversation${qs({ dir, after: o })}`),
  sessionSend: (id: string, text: string, timeoutMs?: number) => http.post<Said>(`/sessions/${seg(id)}/messages`, { text }, timeoutMs),
  sessionKey: (id: string, key: string) => http.post<Said>(`/sessions/${seg(id)}/keystrokes`, { key }),
  sessionClose: (id: string) => http.del<Said>(`/sessions/${seg(id)}/process`),
  open: (b: { dir: string; id?: string; args?: string; prompt?: string }) =>
    b.id ? http.put<Said>(`/sessions/${seg(b.id)}/process${qs({ dir: b.dir })}`, { args: b.args, prompt: b.prompt }) : http.put<Said>(`/projects/${seg(b.dir)}/process`, { args: b.args, prompt: b.prompt }),
  newSession: (b: { dir?: string; id?: string; cwd?: string; prompt: string; args?: string }) => http.post<Said>("/sessions", b),
  rename: (s: Item, b: { title?: string; auto?: boolean }) => http.patch<Said & { title: string }>(`/sessions/${seg(s.id)}${qs({ dir: s.dir })}`, b),
  reassign: (items: Item[], to: string) => inBatches(items, (b) => http.patch<Said & { n: number }>("/sessions", { items: b, to })),
  rmSessions: (items: Item[]) =>
    inBatches(items, (b) => http.del<Said & { deleted: number; spared: number }>(`/sessions${qs({ item: b.map((i) => `${i.dir}:${i.id}`) })}`), (i) => itemLen(`${i.dir}:${i.id}`)),
  trashSessionsAdd: (items: Item[]) => inBatches(items, (b) => http.post<Said>(`/trash/sessions${qs({ close: true })}`, { items: b })),
  trashSessionsRestore: (ids: string[]) => inBatches(ids, (b) => http.patch<Said>(`/trash/sessions${qs({ item: b })}`, { trashed: false }), itemLen),
  trashSessionsPurge: (ids: string[]) => inBatches(ids, (b) => http.del<Said>(`/trash/sessions${qs({ item: b })}`), itemLen),
  vgroups: (b: { groups: VGroup[]; favs: string[]; projects?: Project[] }) => http.put("/vgroups", b),
  dirsAdd: (dir: string, children = false) => http.post<Said & { n: number }>("/directories", { dir, children }),
  dirsRemove: (dir: string) => http.del<Said>(`/directories${qs({ dir })}`),
  updateInfo: () => http.get<{ current: string; latest: string; url: string; newer: boolean }>("/update"),
  keepWarmSet: (b: { dir: string; id: string; on: boolean; max?: number; reheat?: boolean }) => http.put<Said>("/keepwarm", b),
  trustAnswer: (dir: string, yes: boolean) => http.post<Said>("/trust/answers", { dir, yes }),
  termOpen: (id: string) => http.post<{ token: string; cols: number; rows: number }>("/terminal", { id }),
  chatStop: (id: string) => http.del<Said>(`/sessions/${seg(id)}/chat`),
  notesSave: (b: { key: string; text?: string; to?: string }) => http.put<Said>("/notes", b),
  notesSentAdd: (b: { key: string; text: string; to: string }) => http.post<{ entry: SentNote }>("/notes/sent", b),
  notesSentRemove: (key: string, id: string) => http.del<Said>(`/notes/sent${qs({ key, id })}`),
  tabsSave: (name: string) => http.put<Said & { n: number }>("/tabs", { name }),
  tabsRm: (at: string) => http.del<Said>(`/tabs/${seg(at)}`),
  gitPush: (dir: string, remote: string) => http.post<Said>("/git/pushes", { dir, remote }, 90_000),
  trashRestore: (id: string) => http.patch<Said & { key: string }>(`/trash/memory/${seg(id)}`, { trashed: false }),
  trashPurge: (id?: string) => http.del<Said>(`/trash/memory${qs(id ? { item: id } : { all: true })}`),
  inviteCreate: (id: string, dir: string, title?: string, name?: string) =>
    http.post<{ token: string; invite: { id: string; seq: number; label: string } }>("/invites", { id, dir, ...(title ? { title } : {}), ...(name ? { name } : {}) }),
  inviteRegen: (id: string, invite: string) => http.put<{ token: string }>(`/invites/${seg(invite)}/token`, { id }),
  inviteMode: (id: string, invite: string, mode: "rw" | "ro") => http.patch(`/invites/${seg(invite)}`, { id, mode }),
  inviteRevoke: (id: string, invite: string) => http.del(`/invites/${seg(invite)}${qs({ id })}`),
  homesSave: (b: { homes: { name: string; dir: string }[]; removed?: string[] }) => http.put("/homes", b),
  homeSelect: (name: string) => http.put("/homes/current", { name }),
  backupExportUrl: (cats: string[]) => v1(`/backup/export${qs({ cats: cats.join(",") })}`),
  backupStage: async (file: File) => {
    const fd = new FormData()
    fd.set("file", file)
    return readResponse<BackupPlan>(await authFetch(v1("/backup/imports"), { method: "POST", body: fd }))
  },
  backupApply: (id: string, picks: { id: string; mode: string }[], targets: Record<string, string>) => http.post<{ results: BackupResult[] }>(`/backup/imports/${seg(id)}/applications`, { picks, targets }),
  backupDrop: (id: string) => http.del(`/backup/imports/${seg(id)}`),
  syncPlan: (from: string, to: string) => http.post<{ items: SyncItem[] }>("/homes/sync-plans", { from, to }),
  syncApply: (from: string, to: string, picks: { id: string; mode: string }[]) => http.post<Said>("/homes/syncs", { from, to, picks }),
  pairCode: () => http.post<{ code: string; url: string; ttl: number }>("/pairing/codes", {}),
  deviceRevoke: (id: string) => http.del(`/devices/${seg(id)}`),
  shares: () => http.get<{ shares: ShareInfo[] }>("/shares"),
  guestMe: () => http.get<{ id: string; dir: string; mode: "rw" | "ro"; csrf: string; handle: string; title: string; invite: string; label: string }>("/guest/me"),
  // Sans X-Gld-Share : la liste concerne le navigateur entier, pas un partage.
  guestList: async (): Promise<GuestShare[]> => readResponse<GuestShare[]>(await fetch(v1("/guest/sessions"))),
  guestLeave: () => http.del("/guest/me"),
  shareEnd: (id: string) => http.del(`/shares/${seg(id)}`),
  sessionLive: (dir: string, id: string) => http.get<LiveState>(`/sessions/${seg(id)}/live${qs({ dir })}`),
  // refresh : relevé demandé par un POST (un GET ne doit rien déclencher), puis lecture de l'état, qui dit « refreshing ».
  account: async (refresh = false) => {
    if (refresh) await http.post("/account/refreshes")
    return http.get<Account>("/account")
  },
  screen: (id: string) => http.get<{ text: string }>(`/sessions/${seg(id)}/screen`),
  termInfo: () => http.get<{ enabled: boolean }>("/terminal"),
  sendInfo: () => http.get<{ ids: string[]; why: string }>("/sessions/send-info"),
  config: () => http.get<CfgOverview>("/config"),
  configEffective: (dir: string) => http.get<{ rows: EffRow[] }>(`/config/effective${qs({ dir })}`),
  configMcp: () => http.get<{ servers: McpServer[] }>("/config/mcp"),
  configPermSuggest: () => http.get<{ suggestions: PermSuggest[] }>("/config/permissions/suggestions"),
  configEdit: (e: CfgEdit) => http.post<CfgReply>("/config/edits", e),
  configRawEdit: (b: { file: string; text: string; dry: boolean }) => http.put<CfgReply>("/config/raw", b),
  configPermImport: (b: { file: string; text: string; mode: string; dry: boolean }) => http.post<CfgReply>("/config/permissions/imports", b),
  configPermExplain: (kind: string, rule: string) => http.post<{ out: string }>("/config/permissions/explanations", { kind, rule }),
  configRestore: (name: string) => http.post<{ out: string }>("/config/restorations", { name }),
  configMcpCheck: () => http.post<{ status: Record<string, string> }>("/config/mcp/checks"),
  memoryHealth: () => http.get<{ issues: Issue[] }>("/memory/health"),
  memoryMatches: (key: string, dir: string) => http.get<{ files?: string[]; total?: number; truncated?: boolean }>(`/memory/matches${qs({ key, dir })}`),
  memorySearch: (q: string) => http.get<{ hits: Record<string, string> }>(`/memory/search${qs({ q })}`),
  tabs: () => http.get<{ saves: TabSave[] }>("/tabs"),
  trash: () => http.get<{ items: Trashed[] }>("/trash/memory"),
  trashFile: (id: string) => http.get<{ text?: string }>(`/trash/memory/${seg(id)}/file`),
  search: (q: string) => http.get<{ hits: Hit[] }>(`/sessions${qs({ q })}`),
}
