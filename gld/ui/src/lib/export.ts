import { i18n } from "@/i18n"
import { api, type Session } from "./api"
import { full } from "./format"

const slug = (t: string) =>
  t
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 60)

// Télécharge une ou plusieurs conversations (sans les outils) en un seul fichier Markdown.
export async function exportMarkdown(items: { s: Session; project: string }[], title?: string) {
  const parts = await Promise.all(
    items.map(async ({ s, project }) => {
      const { msgs } = await api.conv(s, false)
      const body = msgs.map((m) => `## ${m.r === "user" ? i18n.t("shell.export.you") : "Claude"}\n\n${m.t}`).join("\n\n")
      return `# ${s.title}\n\n_${project} · ${full(s.mtime)} · claude --resume ${s.id}_\n\n${body}\n`
    })
  )
  const md = (title ? `# ${title}\n\n` : "") + parts.join("\n---\n\n")
  const a = document.createElement("a")
  a.href = URL.createObjectURL(new Blob([md], { type: "text/markdown;charset=utf-8" }))
  a.download = `${slug(title ?? items[0].s.title) || items[0].s.id}.md`
  a.click()
  URL.revokeObjectURL(a.href)
}
