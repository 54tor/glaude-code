import { useState } from "react"

// État mémorisé dans le navigateur (localStorage). Sans stockage disponible, il reste un simple état.
export function useLocal(key: string, init: string): [string, (v: string) => void] {
  const [storedValue, setStoredValue] = useState(() => {
    try {
      return localStorage.getItem(key) ?? init
    } catch {
      return init
    }
  })
  return [
    storedValue,
    (nextValue: string) => {
      setStoredValue(nextValue)
      try {
        localStorage.setItem(key, nextValue)
      } catch {
        /* stockage indisponible : on ignore */
      }
    },
  ]
}
