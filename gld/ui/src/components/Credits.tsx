import { useState } from "react"
import QRCode from "react-qr-code"
import { Bug, ChevronRight, Coffee, Copy, ExternalLink, GitFork, Heart, Star } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Tip } from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { copyText } from "@/lib/utils"

// Adresses de soutien : vides, la ligne correspondante n'est pas affichée.
const XMR = "869vB9woRhE1EHMHuYxho3JUj1kooxPDc1ejRw1999kNddapW2qPh9A1G7467itLQ5XYhFKFHp8cw3S53Ut62XtBB2dpx6W"
const REPO = "https://github.com/54tor/glaude-code"

const GO = [
  ["modernc.org/sqlite", "https://gitlab.com/cznic/sqlite"],
  ["coder/websocket", "https://github.com/coder/websocket"],
  ["creack/pty", "https://github.com/creack/pty"],
  ["bmatcuk/doublestar", "https://github.com/bmatcuk/doublestar"],
  ["go.yaml.in/yaml", "https://github.com/yaml/go-yaml"],
  ["fsnotify", "https://github.com/fsnotify/fsnotify"],
  ["go-webauthn", "https://github.com/go-webauthn/webauthn"],
  ["pquerna/otp", "https://github.com/pquerna/otp"],
  ["zalando/go-keyring", "https://github.com/zalando/go-keyring"],
  ["go-i18n", "https://github.com/nicksnyder/go-i18n"],
  ["gofakeit", "https://github.com/brianvoe/gofakeit"],
  ["golang.org/x (crypto, mod, sync, sys, term, text)", "https://pkg.go.dev/golang.org/x"],
]
const WEB = [
  ["React", "https://react.dev"],
  ["Vite", "https://vite.dev"],
  ["Tailwind CSS", "https://tailwindcss.com"],
  ["Radix UI / shadcn", "https://ui.shadcn.com"],
  ["lucide", "https://lucide.dev"],
  ["wouter", "https://github.com/molefrog/wouter"],
  ["react-markdown, remark-gfm", "https://github.com/remarkjs/react-markdown"],
  ["Monaco Editor", "https://microsoft.github.io/monaco-editor"],
  ["i18next, react-i18next", "https://www.i18next.com"],
  ["react-qr-code", "https://github.com/rosskhanas/react-qr-code"],
  ["next-themes", "https://github.com/pacocoursey/next-themes"],
  ["class-variance-authority", "https://cva.style"],
  ["xterm.js", "https://xtermjs.org"],
  ["sonner", "https://sonner.emilkowal.ski"],
  ["cmdk", "https://github.com/dip/cmdk"],
  ["fflate", "https://github.com/101arrowz/fflate"],
  ["jsdiff", "https://github.com/kpdecker/jsdiff"],
  ["Geist, Inter, IBM Plex, JetBrains Mono, Fira Code, Comic Relief", "https://fontsource.org"],
  ["RealFaviconGenerator", "https://realfavicongenerator.net"],
]

const link = "underline-offset-2 hover:underline"

function Libs({ title, rows }: { title: string; rows: string[][] }) {
  return (
    <div>
      <h4 className="mb-1 text-xs font-medium text-muted-foreground">{title}</h4>
      <p className="text-sm leading-6">
        {rows.map(([libraryName, libraryUrl], rowIndex) => (
          <span key={libraryName}>
            {rowIndex > 0 && ", "}
            <a href={libraryUrl} target="_blank" rel="noreferrer" className={link}>
              {libraryName}
            </a>
          </span>
        ))}
      </p>
    </div>
  )
}

function Addr({ label, name, value, scheme }: { label: string; name: string; value: string; scheme: string }) {
  const { t } = useTranslation()
  if (!value) return null
  const copy = () =>
    copyText(value).then(
      () => toast.success(t("shell.credits.copied", { name })),
      () => toast.error(t("shell.credits.copyFailed")),
    )
  return (
    <div className="rounded-lg border bg-muted/30 p-3">
      <div className="mb-1.5 flex items-center gap-2 text-sm font-medium">
        <span className="rounded-sm bg-primary px-1.5 py-0.5 font-mono text-xs text-primary-foreground">{label}</span> {name}
        <a href={`${scheme}:${value}`} className="ml-auto flex items-center gap-1 text-xs font-normal text-muted-foreground hover:text-foreground" title={t("shell.credits.walletHint")}>
          {t("shell.credits.wallet")} <ExternalLink className="size-3" />
        </a>
      </div>
      <div className="flex items-start gap-3">
        <QRCode value={`${scheme}:${value}`} size={96} level="M" className="shrink-0 rounded-sm bg-white p-1" aria-label={t("shell.credits.qr", { name })} />
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <code className="font-mono text-xs leading-5 break-all select-all">{value}</code>
          <Button variant="outline" size="xs" className="self-start" onClick={copy} aria-label={t("shell.credits.copyAddress", { name })}>
            <Copy /> {t("common.copy")}
          </Button>
        </div>
      </div>
    </div>
  )
}

function Way({ icon: Icon, title, text, href }: { icon: typeof Star; title: string; text: string; href: string }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="flex items-start gap-3 rounded-lg border p-3 transition-colors hover:bg-muted">
      <Icon className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block text-xs text-muted-foreground">{text}</span>
      </span>
      <ExternalLink className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
    </a>
  )
}

export function CreditsButton() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Tip label={t("shell.credits.support")}>
        <DialogTrigger asChild>
          <Button variant="ghost" size="icon" className="size-9 text-muted-foreground" aria-label={t("shell.credits.support")}>
            <Heart className="size-6" />
          </Button>
        </DialogTrigger>
      </Tip>
      <DialogContent className="max-h-[90vh] gap-5 overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-xl">
            <Heart className="size-5 text-primary" aria-hidden /> {t("shell.credits.support")}
          </DialogTitle>
          <DialogDescription>{t("shell.credits.intro")}</DialogDescription>
        </DialogHeader>

        <section className="space-y-2" aria-label={t("shell.credits.freeLabel")}>
          <h3 className="text-sm font-semibold">{t("shell.credits.free")}</h3>
          <div className="grid gap-2 sm:grid-cols-2">
            <Way icon={Star} title={t("shell.credits.starTitle")} text={t("shell.credits.starText")} href={REPO} />
            <Way icon={Bug} title={t("shell.credits.bugTitle")} text={t("shell.credits.bugText")} href={`${REPO}/issues`} />
            <Way icon={GitFork} title={t("shell.credits.codeTitle")} text={t("shell.credits.codeText")} href={`${REPO}/pulls`} />
          </div>
        </section>

        {XMR && (
          <section className="space-y-2" aria-label={t("shell.credits.donation")}>
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <Coffee className="size-4" aria-hidden /> {t("shell.credits.coffee")}
            </h3>
            <p className="text-xs text-muted-foreground">{t("shell.credits.donationNote")}</p>
            <Addr label="XMR" name="Monero" value={XMR} scheme="monero" />
          </section>
        )}

        <details className="group/credits rounded-lg border">
          <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm font-medium select-none [&::-webkit-details-marker]:hidden">
            <ChevronRight className="size-4 transition-transform group-open/credits:rotate-90" aria-hidden />
            {t("shell.credits.libs")}
          </summary>
          <div className="space-y-3 border-t px-3 py-3">
            <Libs title={t("shell.credits.server")} rows={GO} />
            <Libs title={t("shell.credits.browser")} rows={WEB} />
          </div>
        </details>
      </DialogContent>
    </Dialog>
  )
}
