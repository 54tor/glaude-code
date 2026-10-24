// notice : raison d'une lecture seule imposée ; tooBig et binary (413, 415) arrivent sans contenu.
export type Notice = "tooBig" | "binary" | "mixedEol"
export type Tab = { path: string; content: string; saved: string; etag: string; readOnly?: boolean; notice?: Notice; conflict?: "changed" | "deleted" }
export type IdeState = { tabs: Tab[]; active: string }
export type Action =
  | { t: "open"; tab: Tab }
  | { t: "close"; path: string }
  | { t: "focus"; path: string }
  | { t: "edit"; path: string; content: string }
  // content : texte effectivement envoyé ; une saisie faite pendant l'enregistrement reste donc sale.
  | { t: "saved"; path: string; etag: string; content?: string }
  // always : signaler même un onglet propre (relecture impossible, le disque ne peut pas être repris seul).
  | { t: "disk"; path: string; kind: "changed" | "deleted"; always?: boolean }
  // force : « Recharger » demandé par l'utilisateur ; sinon un onglet devenu sale entre-temps n'est pas écrasé, il passe en conflit.
  | { t: "reloaded"; path: string; content: string; etag: string; notice?: "mixedEol"; force?: boolean }
  | { t: "renamed"; from: string; to: string }

export const isDirty = (t: Tab) => t.content !== t.saved

const patch = (s: IdeState, path: string, f: (t: Tab) => Tab): IdeState => ({ ...s, tabs: s.tabs.map((t) => (t.path === path ? f(t) : t)) })

export function reduce(s: IdeState, a: Action): IdeState {
  switch (a.t) {
    case "open":
      return s.tabs.some((t) => t.path === a.tab.path) ? { ...s, active: a.tab.path } : { tabs: [...s.tabs, a.tab], active: a.tab.path }
    case "focus":
      return s.tabs.some((t) => t.path === a.path) ? { ...s, active: a.path } : s
    case "close": {
      const i = s.tabs.findIndex((t) => t.path === a.path)
      if (i < 0) return s
      const tabs = s.tabs.filter((t) => t.path !== a.path)
      return { tabs, active: s.active !== a.path ? s.active : (tabs[i] ?? tabs[i - 1])?.path ?? "" }
    }
    case "edit":
      // Un onglet en lecture seule ne devient jamais sale, même si l'éditeur renormalise son texte.
      return patch(s, a.path, (t) => (t.readOnly ? t : { ...t, content: a.content }))
    case "saved":
      return patch(s, a.path, (t) => ({ ...t, conflict: undefined, saved: a.content ?? t.content, etag: a.etag }))
    case "disk": {
      // Un onglet sale n'est jamais rechargé : on signale. Propre et modifié : le composant recharge.
      const t = s.tabs.find((x) => x.path === a.path)
      if (!t || (a.kind === "changed" && !isDirty(t) && !a.always)) return s
      return patch(s, a.path, (x) => ({ ...x, conflict: a.kind }))
    }
    case "reloaded":
      return patch(s, a.path, (t) => (isDirty(t) && !a.force ? { ...t, conflict: "changed" } : { ...t, conflict: undefined, content: a.content, saved: a.content, etag: a.etag, ...(a.notice && { readOnly: true, notice: a.notice }) }))
    case "renamed": {
      const move = (p: string) => (p === a.from ? a.to : p.startsWith(a.from + "/") ? a.to + p.slice(a.from.length) : p)
      return { tabs: s.tabs.map((t) => ({ ...t, path: move(t.path) })), active: move(s.active) }
    }
  }
}
