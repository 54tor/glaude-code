import { useState } from "react"

// État mémorisé dans le navigateur (localStorage). Sans stockage disponible, il reste un simple état.
export function useLocal(key: string, init: string): [string, (v: string) => void] {
  const [v, setV] = useState(() => {
    try {
      return localStorage.getItem(key) ?? init
    } catch {
      return init
    }
  })
  return [
    v,
    (x: string) => {
      setV(x)
      try {
        localStorage.setItem(key, x)
      } catch {
        /* stockage indisponible : on ignore */
      }
    },
  ]
}
