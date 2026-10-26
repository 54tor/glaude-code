import { i18n } from "@/i18n"
import { api, type Item, type Msg, type Session } from "./api"
import { full } from "./format"

const slug = (t: string) =>
  t
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 60)

// Toute la conversation, page par page depuis la fin : le serveur ne renvoie plus un transcript de plusieurs centaines de Mo
// d'un bloc. Pas de `limit` : les pages d'export gardent les textes entiers.
export async function wholeConv(s: Item): Promise<Msg[]> {
  const pages: Msg[][] = []
  let before = 0
  for (;;) {
    const r = await api.conv(s, false, { before })
    pages.unshift(r.msgs)
    if (!r.more || !r.start) return pages.flat()
    before = r.start
  }
}

// Télécharge une ou plusieurs conversations (sans les outils) en un seul fichier Markdown.
export async function exportMarkdown(items: { s: Session; project: string }[], title?: string) {
  const parts = await Promise.all(
    items.map(async ({ s, project }) => {
      const msgs = await wholeConv(s)
      const body = msgs.map((m) => `## ${m.r === "user" ? i18n.t("shell.export.you") : "Claude"}\n\n${m.t}`).join("\n\n")
      return `# ${s.title}\n\n_${project} · ${full(s.mtime)} · claude --resume ${s.id}_\n\n${body}\n`
    })
  )
  const md = (title ? `# ${title}\n\n` : "") + parts.join("\n---\n\n")
  const a = document.createElement("a")
  a.href = URL.createObjectURL(new Blob([md], { type: "text/markdown;charset=utf-8" }))
  a.download = `${slug(title ?? items[0].s.title) || items[0].s.id}.md`
  a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}
