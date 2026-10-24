// notice : raison d'un onglet en lecture seule ouvert sans contenu (413, 415).
export type Tab = { path: string; content: string; saved: string; etag: string; readOnly?: boolean; notice?: "tooBig" | "binary"; conflict?: "changed" | "deleted" }
export type IdeState = { tabs: Tab[]; active: string }
export type Action =
  | { t: "open"; tab: Tab }
  | { t: "close"; path: string }
  | { t: "focus"; path: string }
  | { t: "edit"; path: string; content: string }
  // content : texte effectivement envoyé ; une saisie faite pendant l'enregistrement reste donc sale.
  | { t: "saved"; path: string; etag: string; content?: string }
  | { t: "disk"; path: string; kind: "changed" | "deleted" }
  | { t: "reloaded"; path: string; content: string; etag: string }
  | { t: "renamed"; from: string; to: string }

export const isDirty = (t: Tab) => t.content !== t.saved

const patch = (s: IdeState, path: string, f: (t: Tab) => Tab): IdeState => ({ ...s, tabs: s.tabs.map((t) => (t.path === path ? f(t) : t)) })

export function reduce(s: IdeState, a: Action): IdeState {
  switch (a.t) {
    case "open":
      return s.tabs.some((t) => t.path === a.tab.path) ? { ...s, active: a.tab.path } : { tabs: [...s.tabs, a.tab], active: a.tab.path }
    case "focus":
      return { ...s, active: a.path }
    case "close": {
      const i = s.tabs.findIndex((t) => t.path === a.path)
      if (i < 0) return s
      const tabs = s.tabs.filter((t) => t.path !== a.path)
      return { tabs, active: s.active !== a.path ? s.active : (tabs[i] ?? tabs[i - 1])?.path ?? "" }
    }
    case "edit":
      return patch(s, a.path, (t) => ({ ...t, content: a.content }))
    case "saved":
      return patch(s, a.path, (t) => ({ ...t, conflict: undefined, saved: a.content ?? t.content, etag: a.etag }))
    case "disk": {
      // Un onglet sale n'est jamais rechargé : on signale. Propre et modifié : le composant recharge.
      const t = s.tabs.find((x) => x.path === a.path)
      if (!t || (a.kind === "changed" && !isDirty(t))) return s
      return patch(s, a.path, (x) => ({ ...x, conflict: a.kind }))
    }
    case "reloaded":
      return patch(s, a.path, (t) => ({ ...t, conflict: undefined, content: a.content, saved: a.content, etag: a.etag }))
    case "renamed": {
      const move = (p: string) => (p === a.from ? a.to : p.startsWith(a.from + "/") ? a.to + p.slice(a.from.length) : p)
      return { tabs: s.tabs.map((t) => ({ ...t, path: move(t.path) })), active: move(s.active) }
    }
  }
}
