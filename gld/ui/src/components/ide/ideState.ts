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

export const isDirty = (tab: Tab) => tab.content !== tab.saved

const patchTab = (state: IdeState, path: string, update: (tab: Tab) => Tab): IdeState => ({
  ...state,
  tabs: state.tabs.map((tab) => (tab.path === path ? update(tab) : tab)),
})

const hasTab = (state: IdeState, path: string) => state.tabs.some((tab) => tab.path === path)

export function reduce(state: IdeState, action: Action): IdeState {
  switch (action.t) {
    case "open":
      if (hasTab(state, action.tab.path)) return { ...state, active: action.tab.path }
      return { tabs: [...state.tabs, action.tab], active: action.tab.path }
    case "focus":
      if (!hasTab(state, action.path)) return state
      return { ...state, active: action.path }
    case "close": {
      const closedIndex = state.tabs.findIndex((tab) => tab.path === action.path)
      if (closedIndex < 0) return state
      const tabs = state.tabs.filter((tab) => tab.path !== action.path)
      if (state.active !== action.path) return { tabs, active: state.active }
      const neighbour = tabs[closedIndex] ?? tabs[closedIndex - 1]
      return { tabs, active: neighbour?.path ?? "" }
    }
    case "edit":
      // Un onglet en lecture seule ne devient jamais sale, même si l'éditeur renormalise son texte.
      return patchTab(state, action.path, (tab) => (tab.readOnly ? tab : { ...tab, content: action.content }))
    case "saved":
      return patchTab(state, action.path, (tab) => ({ ...tab, conflict: undefined, saved: action.content ?? tab.content, etag: action.etag }))
    case "disk": {
      // Un onglet sale n'est jamais rechargé : on signale. Propre et modifié : le composant recharge.
      const tab = state.tabs.find((candidate) => candidate.path === action.path)
      if (!tab) return state
      const reloadsSilently = action.kind === "changed" && !isDirty(tab) && !action.always
      if (reloadsSilently) return state
      return patchTab(state, action.path, (current) => ({ ...current, conflict: action.kind }))
    }
    case "reloaded":
      return patchTab(state, action.path, (tab) => {
        if (isDirty(tab) && !action.force) return { ...tab, conflict: "changed" }
        return {
          ...tab,
          conflict: undefined,
          content: action.content,
          saved: action.content,
          etag: action.etag,
          ...(action.notice && { readOnly: true, notice: action.notice }),
        }
      })
    case "renamed": {
      const movedPath = (path: string) => {
        if (path === action.from) return action.to
        if (path.startsWith(action.from + "/")) return action.to + path.slice(action.from.length)
        return path
      }
      return { tabs: state.tabs.map((tab) => ({ ...tab, path: movedPath(tab.path) })), active: movedPath(state.active) }
    }
  }
}
