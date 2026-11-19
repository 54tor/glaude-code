const MOUSE_EVENTS = ["mousedown", "mouseup", "click", "auxclick"] as const

// Ctrl/Cmd+clic: Claude Code ouvre le lien ou le fichier cliqué sur l'hôte; la capture l'arrête avant xterm.
export function blockModifiedClicks(container: HTMLElement): () => void {
  const swallow = (mouseEvent: MouseEvent) => {
    if (mouseEvent.ctrlKey || mouseEvent.metaKey) mouseEvent.stopPropagation()
  }
  MOUSE_EVENTS.forEach((eventName) =>
    container.addEventListener(eventName, swallow, true)
  )
  return () =>
    MOUSE_EVENTS.forEach((eventName) =>
      container.removeEventListener(eventName, swallow, true)
    )
}
