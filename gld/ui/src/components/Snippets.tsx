import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowLeft, Bot, Check, Copy, Download, Fan, Merge, MessageSquarePlus, Pencil, Plus, Save, Sparkles, Terminal, Trash2, Undo2, Unlink, Upload, X } from "lucide-react"
import { toast } from "sonner"
import { api, type SnipScope, type Snippet } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { cn, copyText } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Choose } from "./Choose"
import type { ComposeAsk } from "./Compose"
import { withChoice } from "@/lib/models"
import type { Ask } from "./Confirm"
import { Markdown } from "./Message"
import type { Ask as PromptAsk } from "./Prompt"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"
import { reportAction } from "@/lib/report"
import { errMsg, qs, readResponse, v1 } from "@/lib/apiError"
import { authFetch } from "@/lib/auth"

// Snippets = commandes Claude Code (/nom) : personnelles (~/.claude/commands) et de chaque projet (.claude/commands).
// Lecture, copie, édition, création, suppression (vers la corbeille), import et export en zip.
export function Snippets({
  files,
  scopes,
  dir,
  sel,
  q,
  home,
  onPick,
  onLaunch,
  onChanged,
  onAsk,
  onPrompt,
  onCompose,
  args,
}: {
  files: Snippet[]
  scopes: SnipScope[]
  dir: string // dossier des commandes personnelles
  sel: string // clé du snippet affiché
  q: string
  home: string
  onPick: (key: string) => void
  onLaunch: (s: Snippet, text: string) => void
  onChanged: () => void
  onAsk: (a: Ask) => void
  onPrompt: (a: PromptAsk) => void
  onCompose: (a: ComposeAsk) => void
  args: string
}) {
  const { t } = useTranslation()
  const f = q.toLowerCase()
  const shown = files.filter((s) => !f || (s.path + " " + s.command + " " + s.description + " " + s.project).toLowerCase().includes(f))
  const [loaded, setLoaded] = useState<{ key: string; text: string; error?: string } | null>(null)
  const [draft, setDraft] = useEditorDraft("snippet")
  const [creating, setCreating] = useState("") // nouveau snippet pas encore enregistré
  const [scope, setScope] = useState("") // emplacement de Nouveau / Importer / Exporter ("" : personnelles)
  const [copied, setCopied] = useState(false)
  const [haiku, setHaiku] = useState(false) // fenêtre de mise en forme ouverte
  const [before, setBefore] = useState<{ key: string; text: string } | null>(null) // texte d'avant la mise en forme
  const [picked, setPicked] = useState<Set<string>>(new Set()) // cochés pour la fusion
  const upload = useRef<HTMLInputElement>(null)
  // Erreur de lecture gardée à part : affichée, jamais éditée ni lancée comme une commande.
  const failed = loaded?.key === sel ? loaded.error : undefined
  const text = loaded?.key === sel && failed === undefined ? loaded.text : null
  const editing = draft?.key === sel ? draft : null
  useLeaveGuard(!!draft && !(draft.key === sel && draft.text === text))
  const snip = files.find((s) => s.key === sel)

  useEffect(() => {
    if (!sel || sel === creating) return
    let live = true
    api.snippet(sel).then(
      (r) => live && setLoaded({ key: sel, text: r.text }),
      (e) => live && setLoaded({ key: sel, text: "", error: errMsg(e) })
    )
    return () => {
      live = false
    }
  }, [sel, creating])

  const label = (sc: string) => (sc ? tilde(scopes.find((x) => x.dir === sc)?.project ?? sc, home) : t("skills.snippets.personal"))
  const prefix = (sc: string) => (sc ? `p/${sc}/` : "u/")

  const formatted = (txt: string) => {
    const cur = editing?.text ?? text ?? ""
    setBefore({ key: sel, text: cur })
    setDraft({ key: sel, text: txt })
  }

  const copyAs = (txt: string, what: "Content" | "Command" | "Path") =>
    copyText(txt).then(
      () => toast.success(t(`skills.snippets.copied${what}`)),
      () => toast.error(t("skills.snippets.copyFailed"))
    )
  const copyContent = async (s: Snippet) => {
    const r = await api.snippet(s.key).catch((e) => void toast.error(errMsg(e) || t("skills.snippets.unreadable")))
    if (r) copyAs(r.text, "Content")
  }
  const copy = () =>
    text !== null &&
    copyText(text).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      },
      () => toast.error(t("skills.snippets.copyFailed"))
    )
  const save = async () => {
    if (!editing) return
    if (!(await reportAction(() => api.snippetSave(sel, editing.text, sel === creating)))) return
    setLoaded({ key: sel, text: editing.text })
    setDraft(null)
    setCreating("")
    onChanged()
  }
  const create = () =>
    onPrompt({
      title: t("skills.snippets.newPromptTitle", { scope: label(scope) }),
      value: "",
      placeholder: t("skills.snippets.newPlaceholder"),
      confirm: t("skills.snippets.create"),
      run: (name) => {
        const key = prefix(scope) + (name.endsWith(".md") ? name : name + ".md")
        if (files.some((s) => s.key === key)) return void toast.error(t("skills.snippets.exists"))
        setCreating(key)
        setDraft({ key, text: "---\ndescription: \n---\n\n" })
        onPick(key)
      },
    })
  const rename = () =>
    onPrompt({
      title: t("skills.snippets.renameTitle", { name: snip?.command ?? sel }),
      value: sel.replace(/^(u\/|p\/[^/]+\/)/, "").replace(/\.md$/, ""),
      placeholder: t("skills.snippets.newPlaceholder"),
      confirm: t("skills.snippets.rename"),
      run: async (name) => {
        const head = /^(u\/|p\/[^/]+\/)/.exec(sel)?.[0] ?? ""
        const to = head + (name.endsWith(".md") ? name : name + ".md")
        if (to === sel) return true
        const ok = await reportAction(() => api.snippetRename(sel, to))
        if (ok) {
          onPick(to)
          onChanged()
        }
        return ok
      },
    })
  const remove = () =>
    onAsk({
      title: t("skills.snippets.deleteTitle", { name: snip?.command ?? sel }),
      body: t("skills.snippets.deleteBody"),
      confirm: t("common.delete"),
      run: async () => {
        if (await reportAction(() => api.snippetRemove([sel]))) {
          onPick("")
          onChanged()
        }
      },
    })
  const toggle = (key: string) =>
    setPicked((p) => {
      const n = new Set(p)
      if (!n.delete(key)) n.add(key)
      return n
    })
  // Fusion par Claude : le serveur rédige le prompt ; la fenêtre de saisie le fait relire avant de lancer la session.
  const merge = async () => {
    const keys = files.filter((s) => picked.has(s.key)).map((s) => s.key)
    const draft = (out?: string) => api.snippetMerge({ keys, out, launch: false })
    let r
    try {
      r = await draft()
    } catch (e) {
      return void toast.error(errMsg(e).trim() || t("skills.snippets.mergeFailed"))
    }
    onCompose({
      id: "snippet-merge:" + keys.join(","),
      title: t("skills.snippets.mergeTitle"),
      subtitle: t("skills.snippets.mergeSubtitle", { n: keys.length }),
      project: "",
      initial: r.prompt,
      send: t("skills.snippets.mergeSend"),
      choice: { model: "haiku", effort: "" }, // tâche mécanique
      outputs: keys.map((k) => ({ key: k, label: files.find((x) => x.key === k)?.command ?? k })),
      reprompt: async (out) => {
        try {
          return (await draft(out)).prompt
        } catch (e) {
          return void toast.error(errMsg(e).trim() || t("skills.snippets.mergeFailed"))
        }
      },
      run: async (text, c, _t, out) => {
        try {
          const m = await api.snippetMerge({ keys, out, launch: true, prompt: text, args: withChoice(args, c) })
          toast.success(m.message.trim() || t("skills.snippets.mergeStarted"))
          setPicked(new Set())
          return true
        } catch (e) {
          toast.error(errMsg(e).trim() || t("skills.snippets.mergeFailed"))
          return false
        }
      },
    })
  }
  // Import : fichiers .md ou archives .zip ; un snippet déjà présent est laissé tel quel.
  const importFiles = async (list: FileList | null) => {
    if (!list?.length) return
    const fd = new FormData()
    fd.set("scope", scope)
    for (const file of list) fd.append("files", file)
    const id = toast.loading(t("skills.snippets.importing"))
    await reportAction(async () => readResponse<{ message: string }>(await authFetch(v1("/snippets/imports"), { method: "POST", body: fd })), { id })
    if (upload.current) upload.current.value = ""
    onChanged()
  }

  // Sections : personnelles d'abord, puis chaque projet qui a des commandes.
  const sections = [["", files.filter((s) => !s.scope)] as const, ...scopes.map((x) => [x.dir, files.filter((s) => s.scope === x.dir)] as const)]

  return (
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <div className={cn("shrink-0 flex-col border-b bg-muted/30 lg:flex lg:max-h-none lg:w-80 lg:flex-none lg:border-r lg:border-b-0", sel ? "hidden" : "flex min-h-0 flex-1")}>
        <div className="space-y-2 border-b px-3 py-2">
          <Choose
            value={scope}
            onChange={setScope}
            options={[["", t("skills.snippets.personal")] as const, ...scopes.map((x) => [x.dir, tilde(x.project, home)] as const)]}
            label={t("skills.snippets.location")}
            className="w-full"
          />
          <div className="flex gap-1">
            <Button variant="outline" size="xs" onClick={create} title={t("skills.snippets.newTitle")}>
              <Plus /> {t("skills.snippets.new")}
            </Button>
            <Button variant="ghost" size="xs" onClick={() => upload.current?.click()} title={t("skills.snippets.importTitle")}>
              <Upload /> {t("skills.snippets.import")}
            </Button>
            <Button variant="ghost" size="xs" asChild title={t("skills.snippets.exportTitle")}>
              <a href={v1(`/snippets/export${qs({ scope })}`)} download>
                <Download /> {t("skills.snippets.export")}
              </a>
            </Button>
            <input ref={upload} type="file" multiple accept=".md,.zip" className="hidden" onChange={(e) => importFiles(e.target.files)} />
          </div>
        </div>
        <ScrollArea className="min-h-0 flex-1">
          <nav className="p-2">
            {sections.map(([sc, list]) => {
              const vis = list.filter((s) => shown.includes(s))
              if (!vis.length && sc) return null
              return (
                <section key={sc || "u"} className="mb-3">
                  <h2 className="truncate px-2 py-1 text-xs font-semibold text-muted-foreground" title={sc ? undefined : dir}>
                    {label(sc)}
                  </h2>
                  {vis.length === 0 && (
                    <p className="px-2 py-1 text-xs text-muted-foreground">{files.length ? t("skills.snippets.noneMatch") : t("skills.snippets.none")}</p>
                  )}
                  <ul className="space-y-px">
                    {vis.map((s) => (
                      <li key={s.key}>
                        <ContextMenu>
                          <ContextMenuTrigger asChild>
                        <button
                              onClick={() => onPick(s.key)}
                              title={s.description || s.path}
                              className={cn(
                                "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
                                s.key === sel ? "bg-accent text-accent-foreground" : "hover:bg-muted",
                                picked.has(s.key) && "ring-1 ring-primary"
                              )}
                            >
                              {picked.has(s.key) ? <Check className="mt-0.5 size-4 shrink-0 text-primary" /> : <Terminal className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
                              <span className="min-w-0">
                                <span className="block truncate font-mono text-sm">{s.command}</span>
                                <span className="block truncate text-xs font-normal text-muted-foreground">
                                  {s.description || `${ago(s.mtime)}, ${size(s.kb)}`}
                                </span>
                              </span>
                            </button>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            <ContextMenuItem onSelect={() => copyContent(s)}>
                              <Copy /> {t("skills.snippets.copyContent")}
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => copyAs(s.command, "Command")}>
                              <Copy /> {t("skills.snippets.copyCommand")}
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => copyAs(s.path, "Path")}>
                              <Copy /> {t("skills.snippets.copyPath")}
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            <ContextMenuItem onSelect={() => toggle(s.key)}>
                              {picked.has(s.key) ? <Unlink /> : <Merge />} {picked.has(s.key) ? t("skills.snippets.unmerge") : t("skills.snippets.mergePick")}
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      </li>
                    ))}
                  </ul>
                </section>
              )
            })}
          </nav>
        </ScrollArea>
        {picked.size > 0 && (
          <div className="flex flex-wrap items-center gap-1 border-t bg-popover px-3 py-2 text-xs">
            <span className="mr-auto font-medium">{t("skills.snippets.picked", { count: picked.size })}</span>
            <Button size="xs" onClick={merge} disabled={picked.size < 2} title={t("skills.snippets.mergeTitleBar")}>
              <Merge /> {t("skills.snippets.merge")}
            </Button>
            <Button variant="ghost" size="xs" onClick={() => setPicked(new Set())}>
              <X /> {t("skills.snippets.clear")}
            </Button>
          </div>
        )}
      </div>
      <div className={cn("min-h-0 min-w-0 flex-1 flex-col lg:flex", sel ? "flex" : "hidden")}>
        {sel && (
          <div className="border-b px-2 py-1.5 lg:hidden">
            <Button variant="ghost" size="sm" onClick={() => onPick("")}>
              <ArrowLeft /> {t("skills.snippets.list")}
            </Button>
          </div>
        )}

        {!sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">
            {t("skills.snippets.pickA")} <code>/nom</code> {t("skills.snippets.pickB")}
          </p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2 border-b bg-muted/40 px-6 py-1.5">
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={sel}>
                {snip ? `${snip.command} · ${snip.path}` : sel.replace(/^(u|p\/[^/]+)\//, "")}
                {sel === creating && ` ${t("skills.snippets.newMark")}`}
              </code>
              {editing ? (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setDraft(null)
                      if (sel === creating) {
                        setCreating("")
                        onPick("")
                      }
                    }}
                  >
                    <X /> {t("common.cancel")}
                  </Button>
                  {before?.key === sel && before.text !== editing.text && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => (setDraft({ key: sel, text: before.text }), setBefore(null))}
                      title={t("skills.snippets.undoTitle")}
                    >
                      <Undo2 /> {t("skills.snippets.undo")}
                    </Button>
                  )}
                  <Button variant="outline" size="sm" onClick={() => setHaiku(true)}>
                    <Bot /> {t("skills.snippets.improve")}
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === text}>
                    <Save /> {t("common.save")}
                  </Button>
                </>
              ) : (
                <>
                  <Button variant="ghost" size="sm" onClick={rename} disabled={text === null}>
                    <Pencil /> {t("skills.snippets.rename")}
                  </Button>
                  <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={remove} disabled={text === null}>
                    <Trash2 /> {t("common.delete")}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => text !== null && setDraft({ key: sel, text })} disabled={text === null}>
                    <Pencil /> {t("skills.snippets.edit")}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setHaiku(true)} disabled={text === null} title={t("skills.snippets.improveTitle")}>
                    <Bot /> {t("skills.snippets.improve")}
                  </Button>
                  <Button variant="outline" size="sm" onClick={copy} disabled={text === null}>
                    {copied ? <Check /> : <Copy />} {copied ? t("common.copied") : t("common.copy")}
                  </Button>
                  <Button size="sm" onClick={() => snip && text !== null && onLaunch(snip, text)} disabled={!snip || text === null}>
                    <MessageSquarePlus /> {t("skills.snippets.launch")}
                  </Button>
                </>
              )}
            </div>
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(e) => setDraft({ key: sel, text: e.target.value })}
                onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), save())}
                spellCheck={false}
                autoFocus
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
                aria-label={t("skills.snippets.editorLabel")}
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-5xl px-6 py-6 text-sm leading-relaxed">
                  {failed !== undefined ? <p className="text-destructive">{failed || t("skills.snippets.unreadable")}</p> : text === null ? <Skeleton className="h-32 w-full" /> : <CommandView text={text} />}
                </div>
              </ScrollArea>
            )}
          </>
        )}
      </div>
      <HaikuFormat open={haiku} text={editing?.text ?? text ?? ""} onClose={() => setHaiku(false)} onDone={formatted} />
    </div>
  )
}

// En-tête YAML (description, argument-hint, model…) en petit, puis le prompt rendu en Markdown.
function CommandView({ text }: { text: string }) {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(text)
  return (
    <>
      {m && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{m[1]}</pre>}
      <Markdown t={m ? text.slice(m[0].length) : text} />
    </>
  )
}

// Mise en forme d'une commande par un petit modèle : le résultat remplace le texte de l'éditeur, sans être enregistré.
function HaikuFormat({ open, text, onClose, onDone }: { open: boolean; text: string; onClose: () => void; onDone: (t: string) => void }) {
  const { t } = useTranslation()
  const models = [
    ["haiku", t("skills.format.modelHaiku")],
    ["sonnet", "Sonnet"],
    ["opus", "Opus"],
  ] as const
  const [instruction, setInstruction] = useState("")
  const [model, setModel] = useState("haiku")
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    const r = await api.snippetDraft({ model, instruction, text }).catch((e) => void toast.error(errMsg(e)))
    setBusy(false)
    if (!r) return
    toast.success(t("skills.format.formatReady"), { description: r.cost ? t("skills.format.cost", { cost: r.cost.toFixed(4) }) : undefined })
    setInstruction("")
    onClose()
    onDone(r.text)
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("skills.format.title")}</DialogTitle>
          <DialogDescription>{t("skills.format.description")}</DialogDescription>
        </DialogHeader>
        <textarea
          value={instruction}
          onChange={(e) => setInstruction(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), run())}
          placeholder={text.trim() ? t("skills.format.placeholderEdit") : t("skills.format.placeholderNew")}
          autoFocus
          rows={3}
          disabled={busy}
          className="w-full resize-y rounded-lg border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
          aria-label={t("skills.format.instruction")}
        />
        <DialogFooter className="flex-wrap items-center gap-2 sm:justify-between">
          <Choose value={model} onChange={setModel} options={models} label={t("skills.format.model")} size="default" />
          <div className="flex gap-2">
            <Button variant="outline" onClick={onClose} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button onClick={run} disabled={busy || (!text.trim() && !instruction.trim())} title={t("skills.format.shortcut")}>
              {busy ? <Fan className="animate-spin" /> : <Sparkles />} {t("skills.format.run")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
