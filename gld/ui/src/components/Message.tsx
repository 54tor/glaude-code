import { loc } from "@/i18n"
import { memo, useState } from "react"
import { useTranslation } from "react-i18next"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { Check, ChevronRight, CornerDownRight, Download, ExternalLink, MessageCircleQuestion, OctagonX, Wrench } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { api, type Msg } from "@/lib/api"
import { authFetch, getAuth } from "@/lib/auth"
import { qs, seg, v1 } from "@/lib/apiError"
import { size } from "@/lib/format"
import { noBreak, rehypeNoBreak } from "@/lib/noBreak"
import { CompactSummary, isCompactSummary, isTagged, Tagged } from "./Tagged"
import { setDraft } from "@/hooks/useDraft"

// Messages de Claude : Markdown (tableaux GFM compris). react-markdown n'interprète jamais de HTML brut
// et filtre les URL dangereuses (javascript:) : rien de ce qui est affiché ne peut s'exécuter.
// onLink : intercepte un lien relatif (ex. une autre entrée de mémoire) ; renvoie false pour le laisser faire.
export function Markdown({ t, onLink }: { t: string; onLink?: (href: string) => boolean }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeNoBreak]}
        components={{
          // URL filtrée (javascript:…) : plus de href, donc du texte simple plutôt qu'un lien qui rouvrirait la page.
          a: ({ href, children }) =>
            href ? (
              <a href={href} target="_blank" rel="noopener noreferrer" onClick={(event) => onLink?.(href) && event.preventDefault()}>
                {children}
              </a>
            ) : (
              <span>{children}</span>
            ),
        }}
      >
        {hideHome(t)}
      </ReactMarkdown>
    </div>
  )
}

// Texte collé dans Claude Code (<pasted_content id="…">…</pasted_content id="…">) : un bloc repliable.
const PASTED = /<pasted_content id="[^"]*">\n?([\s\S]*?)\n?<\/pasted_content(?: id="[^"]*")?>/g

// Le dossier personnel (et donc l'identifiant du compte) ne s'affiche pas : « @/home/nom/x.png » devient « @~/x.png ».
const HOME_PREFIX = /(^|[\s@("'`])(?:\/home\/[^/\s]+|\/Users\/[^/\s]+|[A-Za-z]:[\\/]Users[\\/][^\\/\s]+)(?=[\\/])/g
export const hideHome = (text: string) => text.replace(HOME_PREFIX, "$1~")

// Image déposée par l'interface (« @/…/.claude/gld-web-images/<nom> ») : affichée au lieu du chemin, relue par son seul nom (GET /v1/sessions/{id}/conversation/uploads/{up}).
export const UPLOADED = /@[^\s@]{0,300}\/gld-web-images\/([A-Za-z0-9][\w.-]*\.(?:png|jpe?g|gif|webp))/g

// Texte tapé par l'utilisateur : affiché tel quel, jamais interprété comme du Markdown (images distantes, liens, mise en forme).
function PlainText({ t: text }: { t: string }) {
  return <p className="break-words whitespace-pre-wrap">{noBreak(hideHome(text))}</p>
}

function UserText({ t: text, conv }: { t: string; conv?: Conv }) {
  const { t } = useTranslation()
  const parts: React.ReactNode[] = []
  const pushMarkdownWithUploads = (markdownText: string, key: string) => {
    if (!conv || conv.guest) {
      parts.push(<PlainText key={key} t={markdownText} />)
      return
    }
    let from = 0
    for (const uploadMatch of markdownText.matchAll(UPLOADED)) {
      const before = markdownText.slice(from, uploadMatch.index).trim()
      if (before) parts.push(<PlainText key={`${key}t${from}`} t={before} />)
      const src = v1(`/sessions/${seg(conv.id)}/conversation/uploads/${seg(uploadMatch[1])}${qs({ dir: conv.dir })}`)
      parts.push(
        <a key={`${key}i${uploadMatch.index}`} href={src} target="_blank" rel="noopener noreferrer" title={t("conversation.message.openImage")} className="my-1 block w-fit">
          <img src={src} loading="lazy" alt={t("conversation.message.imageAlt")} className="max-h-80 max-w-full rounded-lg border bg-muted/30 object-contain" />
        </a>
      )
      from = uploadMatch.index + uploadMatch[0].length
    }
    const rest = markdownText.slice(from).trim()
    if (rest) parts.push(<PlainText key={`${key}r`} t={rest} />)
  }
  let at = 0
  for (const pastedMatch of text.matchAll(PASTED)) {
    const before = text.slice(at, pastedMatch.index).trim()
    if (before) pushMarkdownWithUploads(before, `t${at}`)
    const pastedContent = pastedMatch[1]
    const lines = pastedContent.split("\n").length
    parts.push(
      <details key={pastedMatch.index} className="group/paste my-1 rounded-md border bg-background/60">
        <summary className="cursor-pointer list-none px-3 py-1.5 text-xs text-muted-foreground select-none hover:text-foreground">
          {t("conversation.message.pasted", { count: lines })}
        </summary>
        <pre className="max-h-96 overflow-auto border-t px-3 py-2 text-xs whitespace-pre-wrap">{pastedContent}</pre>
      </details>
    )
    at = pastedMatch.index + pastedMatch[0].length
  }
  const rest = text.slice(at).trim()
  if (rest) pushMarkdownWithUploads(rest, "rest")
  return <>{parts}</>
}

// Spécification ou plan superpowers cité par un appel d'outil : lu à la demande dans le dossier de la session et rendu en Markdown.
const SP_DOC = /docs\/superpowers\/(?:specs|plans)\/[^\s"']+\.md/
function SuperpowersDoc({ dir, path }: { dir: string; path: string }) {
  const { t } = useTranslation()
  const [doc, setDoc] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const flip = () => {
    setOpen(!open)
    if (doc !== null) return
    const fail = () => {
      toast.error(t("conversation.message.docFail"))
      setOpen(false)
    }
    api.fsFile(dir, path).then((file) => setDoc(file.content), fail)
  }
  return (
    <div className="mt-1">
      <button type="button" onClick={flip} aria-expanded={open} className="flex cursor-pointer items-center gap-1 text-xs text-primary hover:underline">
        <ChevronRight className={`size-3.5 transition-transform ${open ? "rotate-90" : ""}`} aria-hidden />
        {t("conversation.message.showDoc")}
      </button>
      {open && doc !== null && (
        <div className="mt-2 rounded-lg border bg-card px-4 py-3 leading-relaxed text-card-foreground">
          <Markdown t={doc} />
        </div>
      )}
    </div>
  )
}

const FRAMED_EXTENSIONS = ["html", "htm", "pdf"]
const IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "gif", "webp"]
const INTERRUPTED = /^\s*\[Request interrupted by user( for tool use)?\]\s*$/
const BYTES_PER_KILOBYTE = 1024

// Fichier écrit par Claude (md, html, image, pdf) : relu à la demande par le serveur, qui ne sert que les chemins du transcript.
export function WrittenDoc({ conv, path }: { conv: Conv; path: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [md, setMd] = useState<string | null>(null)
  const src = v1(`/sessions/${seg(conv.id)}/conversation/document${qs({ dir: conv.dir, path })}`)
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase()
  const isMarkdown = ext === "md"
  const isFramed = FRAMED_EXTENSIONS.includes(ext)
  const isImage = IMAGE_EXTENSIONS.includes(ext)
  const downloadSrc = isMarkdown ? v1(`/sessions/${seg(conv.id)}/conversation/document${qs({ dir: conv.dir, path, download: true })}`) : src
  const fileName = path.slice(path.lastIndexOf("/") + 1)
  const flip = () => {
    setOpen(!open)
    if (!isMarkdown || md !== null) return
    const fail = () => {
      toast.error(t("conversation.message.docFail"))
      setOpen(false)
    }
    authFetch(src).then((response) => {
      if (response.ok) return response.text().then(setMd)
      return fail()
    }, fail)
  }
  return (
    <div className="msg">
      <div className="flex items-center gap-2 text-xs">
        <button type="button" onClick={flip} aria-expanded={open} className="flex min-w-0 cursor-pointer items-center gap-1 text-primary hover:underline">
          <ChevronRight className={`size-3.5 shrink-0 transition-transform ${open && !isMarkdown ? "rotate-90" : ""}`} aria-hidden />
          <span className="truncate" title={path}>{fileName}</span>
        </button>
        {isMarkdown ? (
          <a href={downloadSrc} download={fileName} title={t("conversation.message.downloadDoc")} className="text-muted-foreground hover:text-foreground">
            <Download className="size-3.5" aria-hidden />
          </a>
        ) : (
          <a href={src} target="_blank" rel="noopener noreferrer" title={t("conversation.message.openImage")} className="text-muted-foreground hover:text-foreground">
            <ExternalLink className="size-3.5" aria-hidden />
          </a>
        )}
      </div>
      {isMarkdown && (
        <Dialog open={open && md !== null} onOpenChange={setOpen}>
          <DialogContent className="flex h-[85vh] max-h-[85vh] min-w-0 flex-col overflow-hidden sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle className="text-sm break-all">{fileName}</DialogTitle>
              <DialogDescription className="sr-only">{t("conversation.message.docDescription")}</DialogDescription>
            </DialogHeader>
            <div className="min-h-0 flex-1 overflow-y-auto leading-relaxed">{md !== null && <Markdown t={md} />}</div>
          </DialogContent>
        </Dialog>
      )}
      {open && isFramed && <iframe src={src} sandbox="" title={path} className="mt-2 h-[32rem] w-full rounded-lg border bg-white" />}
      {open && isImage && <img src={src} alt={path} className="mt-2 max-h-96 max-w-full rounded-lg border object-contain" />}
    </div>
  )
}

// Mémoïsé (voir plus bas) : la conversation se redessine pour l'état de la session, pas pour chacun de ses messages.
// guest : vue invitée, sans document ni lecture de fichier (routes refusées à l'invité).
export type Conv = { dir: string; id: string; tools?: boolean; guest?: boolean }

function MessageView({ m: message, conv }: { m: Msg; conv?: Conv }) {
  const { t } = useTranslation()
  const title = message.ts ? new Date(message.ts).toLocaleString(loc()) : undefined
  // Image de la conversation : relue à la demande dans le transcript (GET /v1/sessions/{id}/conversation/images/{img}), jamais embarquée dans la liste des messages.
  if (message.img && conv) {
    // Un <img> n'envoie pas X-Gld-Share : l'invité nomme son partage dans l'URL.
    const share = conv.guest ? getAuth().share : undefined
    const src = v1(`/sessions/${seg(conv.id)}/conversation/images/${message.o ?? 0}-${message.k ?? 0}${qs({ dir: conv.dir, share })}`)
    return (
      <div title={title} className={`msg flex ${message.r === "user" ? "justify-end" : ""}`}>
        <a href={src} target="_blank" rel="noopener noreferrer" title={t("conversation.message.openImage")}>
          <img src={src} loading="lazy" alt={t("conversation.message.imageAlt")} className="max-h-80 max-w-full rounded-xl border bg-muted/30 object-contain" />
        </a>
      </div>
    )
  }
  const canReadFiles = conv && !conv.guest
  if (message.r === "doc") return canReadFiles ? <WrittenDoc conv={conv} path={message.t} /> : null
  if (message.r === "user" && isCompactSummary(message.t))
    return (
      <div title={title} className="msg mx-auto max-w-3xl">
        <CompactSummary t={message.t} md={(markdownText) => <Markdown t={markdownText} />} />
      </div>
    )
  const stop = message.r === "user" ? INTERRUPTED.exec(message.t) : null
  if (stop)
    return (
      <div title={title} role="status" className="msg mx-auto flex w-fit items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-1.5 text-sm font-medium text-destructive">
        <OctagonX className="size-4" aria-hidden /> {t(stop[1] ? "conversation.message.interruptedTool" : "conversation.message.interrupted")}
      </div>
    )
  if (message.r === "user" && isTagged(message.t))
    return (
      <div title={title} className="msg max-w-[85%]">
        <Tagged
          t={message.t}
          md={(markdownText) => <Markdown t={markdownText} />}
          onReply={canReadFiles ? (to, name) => setDraft(conv.id, { text: t("conversation.tagged.replyDraft", { name, to }) }) : undefined}
        />
      </div>
    )
  if (message.r === "user")
    return (
      <div title={title} className="msg flex justify-end">
        <div className="max-w-[85%] rounded-lg bg-accent px-4 py-2.5 break-words text-accent-foreground">
          <UserText t={message.t} conv={conv} />
        </div>
      </div>
    )
  if (message.r === "ask") return <AskAnswers title={title} json={message.t} />
  if (message.r === "tool") {
    // « ↳ … » : résultat d'un appel ; sinon l'appel lui-même (« Outil  argument »).
    const result = message.t.startsWith("↳")
    const Icon = result ? CornerDownRight : Wrench
    return (
      <div title={title} className="msg flex gap-2 border-l-2 border-primary/30 py-0.5 pl-3 text-muted-foreground">
        <Icon className="mt-0.5 size-3.5 shrink-0" aria-label={result ? t("conversation.message.toolResult") : t("conversation.message.toolCall")} />
        <div className="min-w-0">
          <span className="break-words whitespace-pre-wrap">{noBreak(hideHome(result ? message.t.slice(1).trimStart() : message.t))}</span>
          {!result && canReadFiles && SP_DOC.test(message.t) && <SuperpowersDoc dir={conv.dir} path={SP_DOC.exec(message.t)![0]} />}
        </div>
      </div>
    )
  }
  return (
    <div title={title} className="msg leading-relaxed break-words">
      <Markdown t={message.t} />
    </div>
  )
}

type AskAnswer = { q: string; a: string }

function parseAskAnswers(json: string): AskAnswer[] {
  try {
    const parsed: unknown = JSON.parse(json)
    return Array.isArray(parsed) ? (parsed as AskAnswer[]) : []
  } catch {
    return []
  }
}

// Réponses données à un AskUserQuestion : une carte, une ligne question/réponse par question.
function AskAnswers({ title, json }: { title?: string; json: string }) {
  const { t } = useTranslation()
  const answers = parseAskAnswers(json)
  return (
    <div title={title} className="msg rounded-lg border border-primary/30 bg-primary/5 px-4 py-3 text-sm">
      <p className="mb-2 flex items-center gap-2 font-medium text-primary">
        <MessageCircleQuestion className="size-4" aria-hidden /> {t("conversation.message.askAnswered")}
      </p>
      <dl className="space-y-2">
        {answers.map((answer, answerIndex) => (
          <div key={answerIndex}>
            <dt className="text-muted-foreground">{answer.q}</dt>
            <dd className="mt-0.5 flex items-start gap-1.5 font-medium break-words whitespace-pre-wrap">
              <Check className="mt-0.5 size-3.5 shrink-0 text-primary" aria-hidden /> {answer.a}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

// Message démesuré (coupé par le serveur) : affiché par morceaux, chacun à la demande.
function CutMessage({ m: message, conv }: { m: Msg; conv?: Conv }) {
  const { t } = useTranslation()
  const [more, setMore] = useState({ t: "", cut: message.cut ?? 0 })
  const [busy, setBusy] = useState(false)
  const next = async () => {
    if (!conv) return
    setBusy(true)
    try {
      const chunk = await api.convText(conv, !!conv.tools, message, more.cut)
      setMore((previous) => ({ t: previous.t + chunk.t, cut: chunk.cut }))
    } catch {
      toast.error(t("conversation.message.readFail"))
    } finally {
      setBusy(false)
    }
  }
  const left = Math.max(1, Math.round(((message.len ?? 0) - more.cut) / BYTES_PER_KILOBYTE))
  return (
    <>
      <MessageView m={more.t ? { ...message, t: message.t + more.t } : message} conv={conv} />
      {more.cut > 0 && conv && (
        <div className={message.r === "user" ? "flex justify-end" : ""}>
          <Button variant="outline" size="sm" disabled={busy} onClick={next}>
            {t("conversation.message.showMore", { size: size(left) })}
          </Button>
        </div>
      )}
    </>
  )
}

type MessageProps = { m: Msg; conv?: Conv }

function MessageOrCut(props: MessageProps) {
  return props.m.cut ? <CutMessage {...props} /> : <MessageView {...props} />
}

function sameMessageProps(previous: MessageProps, next: MessageProps) {
  return (
    previous.m === next.m &&
    previous.conv?.dir === next.conv?.dir &&
    previous.conv?.id === next.conv?.id &&
    previous.conv?.tools === next.conv?.tools
  )
}

export const Message = memo(MessageOrCut, sameMessageProps)
