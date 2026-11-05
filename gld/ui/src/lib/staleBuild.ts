import { useEffect, useState } from "react"

const ENTRY_SCRIPT_PATTERN = /\/assets\/app-[\w-]+\.js/
const CHECK_INTERVAL_MS = 5 * 60 * 1000

const runningEntryScript = () => document.querySelector<HTMLScriptElement>('script[type="module"][src*="/assets/app-"]')?.getAttribute("src") ?? ""

// Le serveur sert la page d'entrée sans cache : si son script principal n'est plus celui qui tourne ici, une version plus récente est déployée.
async function deployedEntryScript(): Promise<string> {
  try {
    const response = await fetch("/", { cache: "no-store", credentials: "same-origin" })
    if (!response.ok) return ""
    return (await response.text()).match(ENTRY_SCRIPT_PATTERN)?.[0] ?? ""
  } catch {
    return ""
  }
}

export function useStaleBuild(): boolean {
  const [isStale, setIsStale] = useState(false)
  useEffect(() => {
    if (!import.meta.env.PROD) return
    const currentScript = runningEntryScript()
    if (!currentScript) return
    const checkDeployedVersion = async () => {
      const deployedScript = await deployedEntryScript()
      if (deployedScript && deployedScript !== currentScript) setIsStale(true)
    }
    const checkWhenVisible = () => {
      if (document.visibilityState === "visible") void checkDeployedVersion()
    }
    document.addEventListener("visibilitychange", checkWhenVisible)
    const intervalId = window.setInterval(checkWhenVisible, CHECK_INTERVAL_MS)
    checkWhenVisible()
    return () => {
      document.removeEventListener("visibilitychange", checkWhenVisible)
      window.clearInterval(intervalId)
    }
  }, [])
  return isStale
}
