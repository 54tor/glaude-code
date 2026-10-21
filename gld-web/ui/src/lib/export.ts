import { api, type Session } from "./api"
import { full } from "./format"

// Télécharge la conversation (sans les outils) en Markdown.
export async function exportMarkdown(s: Session, project: string) {
  const { msgs } = await api.conv(s, false)
  const body = msgs.map((m) => `## ${m.r === "user" ? "Vous" : "Claude"}\n\n${m.t}`).join("\n\n")
  const md = `# ${s.title}\n\n_${project} · ${full(s.mtime)} · claude --resume ${s.id}_\n\n${body}\n`
  const a = document.createElement("a")
  a.href = URL.createObjectURL(new Blob([md], { type: "text/markdown;charset=utf-8" }))
  a.download = `${
    s.title
      .replace(/[^\p{L}\p{N}]+/gu, "-")
      .replace(/^-|-$/g, "")
      .slice(0, 60) || s.id
  }.md`
  a.click()
  URL.revokeObjectURL(a.href)
}
