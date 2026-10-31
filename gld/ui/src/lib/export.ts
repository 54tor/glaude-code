import { i18n } from "@/i18n"
import { api, type Item, type Msg, type Session } from "./api"
import { full } from "./format"

const SLUG_MAX_LENGTH = 60
const OBJECT_URL_LIFETIME_MS = 1000

const slug = (title: string) =>
  title
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-|-$/g, "")
    .slice(0, SLUG_MAX_LENGTH)

// Toute la conversation, page par page depuis la fin : le serveur ne renvoie plus un transcript de plusieurs centaines de Mo
// d'un bloc. Pas de `limit` : les pages d'export gardent les textes entiers.
export async function wholeConv(session: Item): Promise<Msg[]> {
  const pages: Msg[][] = []
  let before = 0
  for (;;) {
    const page = await api.conv(session, false, { before })
    pages.unshift(page.msgs)
    if (!page.more || !page.start) return pages.flat()
    before = page.start
  }
}

// Télécharge une ou plusieurs conversations (sans les outils) en un seul fichier Markdown.
export async function exportMarkdown(items: { s: Session; project: string }[], title?: string) {
  const authorHeading = (message: Msg) => (message.r === "user" ? i18n.t("shell.export.you") : "Claude")
  const parts = await Promise.all(
    items.map(async ({ s: session, project }) => {
      const messages = await wholeConv(session)
      const body = messages.map((message) => `## ${authorHeading(message)}\n\n${message.t}`).join("\n\n")
      return `# ${session.title}\n\n_${project} · ${full(session.mtime)} · claude --resume ${session.id}_\n\n${body}\n`
    })
  )
  const titleHeading = title ? `# ${title}\n\n` : ""
  const markdown = titleHeading + parts.join("\n---\n\n")
  const firstSession = items[0].s
  const downloadLink = document.createElement("a")
  downloadLink.href = URL.createObjectURL(new Blob([markdown], { type: "text/markdown;charset=utf-8" }))
  downloadLink.download = `${slug(title ?? firstSession.title) || firstSession.id}.md`
  downloadLink.click()
  setTimeout(() => URL.revokeObjectURL(downloadLink.href), OBJECT_URL_LIFETIME_MS)
}
