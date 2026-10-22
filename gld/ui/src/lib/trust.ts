import { toast } from "sonner"
import { api } from "@/lib/api"
import { tilde } from "@/lib/format"

const asked = new Set<string>() // dossiers dont la question est déjà affichée

// Une session lancée dans un dossier neuf attend, dans son terminal, la réponse au dialogue de confiance de Claude Code. On guette
// ce dialogue quelques secondes et on pose la question ici ; la réponse est envoyée au terminal.
export async function watchTrust(home: string) {
  for (let i = 0; i < 25; i++) {
    await new Promise((r) => setTimeout(r, 1000))
    const { dirs } = await api.trustPending().catch(() => ({ dirs: [] as string[] }))
    for (const dir of dirs.filter((d) => !asked.has(d))) {
      asked.add(dir)
      const answer = (yes: boolean) =>
        api.post("/trust-answer", { dir, yes }).then((r) => {
          asked.delete(dir)
          if (!r.ok) toast.error(r.out)
        })
      toast(`Faire confiance à ${tilde(dir, home)} ?`, {
        description: "Claude Code demande s'il peut lire et exécuter les fichiers de ce dossier. À n'accepter que pour un dossier que tu connais.",
        duration: Infinity,
        action: { label: "Faire confiance", onClick: () => void answer(true) },
        cancel: { label: "Refuser", onClick: () => void answer(false) },
        onDismiss: () => asked.delete(dir),
      })
    }
    if (dirs.length > 0) return
  }
}
