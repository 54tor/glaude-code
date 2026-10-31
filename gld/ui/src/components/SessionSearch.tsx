import { FileText, Fan, Search, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

// Recherche d'une vue de sessions : par nom, ou (mode avancé) aussi dans le texte des conversations.
export function SessionSearch({ q: query, setQ, adv, setAdv, searching, memory }: { q: string; setQ: (localValue: string) => void; adv: boolean; setAdv: (flag: boolean) => void; searching: boolean; memory?: boolean }) {
  const { t } = useTranslation()
  const clearAndBlurOnEscape = (keyboardEvent: React.KeyboardEvent<HTMLInputElement>) => {
    if (keyboardEvent.key !== "Escape") return
    setQ("")
    keyboardEvent.currentTarget.blur()
  }
  return (
    <div className="relative w-full md:w-96">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        value={query}
        onChange={(changeEvent) => setQ(changeEvent.target.value)}
        onKeyDown={clearAndBlurOnEscape}
        placeholder={memory ? t("shell.search.placeholderMemory") : adv ? t("shell.search.placeholderContent") : t("shell.search.placeholder")}
        aria-label={t("shell.search.label")}
        className="h-7 bg-background pr-12 pl-8 shadow-none md:pr-28"
      />
      <div className="absolute top-1/2 right-1 flex -translate-y-1/2 items-center gap-1">
        {searching ? (
          <Fan className="size-4 animate-spin text-muted-foreground" aria-label={t("shell.sidebar.searching")} />
        ) : (
          query && (
            <button className="text-muted-foreground hover:text-foreground" onClick={() => setQ("")} aria-label={t("shell.sidebar.clearSearch")}>
              <X className="size-4" />
            </button>
          )
        )}
        {!memory && <Button size="xs" variant={adv ? "secondary" : "ghost"} aria-pressed={adv} onClick={() => setAdv(!adv)} title={t("shell.search.contentHint")}>
          <FileText /> <span className="max-md:sr-only">{t("shell.search.content")}</span>
        </Button>}
      </div>
    </div>
  )
}
