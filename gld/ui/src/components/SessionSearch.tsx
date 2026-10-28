import { FileText, LoaderPinwheel, Search, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

// Recherche d'une vue de sessions : par nom, ou (mode avancé) aussi dans le texte des conversations.
export function SessionSearch({ q, setQ, adv, setAdv, searching }: { q: string; setQ: (v: string) => void; adv: boolean; setAdv: (v: boolean) => void; searching: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="relative w-full md:w-96">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && (setQ(""), e.currentTarget.blur())}
        placeholder={adv ? t("shell.search.placeholderContent") : t("shell.search.placeholder")}
        aria-label={t("shell.search.label")}
        className="h-8 bg-background pr-12 pl-8 shadow-none md:pr-28"
      />
      <div className="absolute top-1/2 right-1 flex -translate-y-1/2 items-center gap-1">
        {searching ? (
          <LoaderPinwheel className="size-4 animate-spin text-muted-foreground" aria-label={t("shell.sidebar.searching")} />
        ) : (
          q && (
            <button className="text-muted-foreground hover:text-foreground" onClick={() => setQ("")} aria-label={t("shell.sidebar.clearSearch")}>
              <X className="size-4" />
            </button>
          )
        )}
        <Button size="xs" variant={adv ? "secondary" : "ghost"} aria-pressed={adv} onClick={() => setAdv(!adv)} title={t("shell.search.contentHint")}>
          <FileText /> <span className="max-md:sr-only">{t("shell.search.content")}</span>
        </Button>
      </div>
    </div>
  )
}
