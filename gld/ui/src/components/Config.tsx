import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type CfgOverview } from "@/lib/api"
import { tilde } from "@/lib/format"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useActions } from "./actions"
import { Choose } from "./Choose"
import { useConfigEdit } from "./configEdit"
import { HealthTab, HooksTab, McpTab, PrecedenceTab } from "./ConfigTabs1"
import { ProjectsTab } from "./ConfigProjects"
import { RawTab } from "./ConfigRaw"
import { BackupsTab, PermsTab, SettingsTab } from "./ConfigTabs2"
import { SyncTab } from "./ConfigSync"
import { takeConfigFile } from "@/lib/wantConfigFile"

// Configuration de Claude Code : réglages utilisateur, de projet et locaux. Lecture (précédence, santé, hooks, MCP) et
// modifications limitées aux réglages simples, variables d'environnement, plugins et permissions, avec aperçu et sauvegarde.
export function Config({ home, tab, onTab }: { home: string; tab: string; onTab: (t: string) => void }) {
  const { t } = useTranslation()
  const [ov, setOv] = useState<CfgOverview | null>(null)
  const [fileId, setFileId] = useState(() => takeConfigFile() || "u") // fichier édité dans « Permissions » et « Réglages »
  const reload = useCallback(() => api.config().then(setOv), [])
  useEffect(() => {
    reload()
  }, [reload])
  const a = useActions()
  const sessions = a.live.size // sessions ouvertes : elles rechargent seules les réglages modifiés
  const { gone, added, moveRule, propose, proposeRaw, proposePerms, dialog } = useConfigEdit(reload, sessions)
  // Direct, sans fenêtre : la règle rejoint vos réglages, puis (déplacement) quitte son fichier d'origine.
  const transfer = async (file: string, kind: string, rule: string, move: boolean) => {
    if (kind === "additionalDirectories") {
      // l'import de règles ignore les dossiers : ajout direct
      await propose({ file: "u", op: "add", path: ["permissions", kind], value: rule })
      if (move) await propose({ file, op: "remove", path: ["permissions", kind], value: rule })
      return
    }
    const r = (await api.post("/config-perm-import", { file: "u", text: JSON.stringify({ permissions: { [kind]: [rule] } }), mode: "merge", dry: false })) as { ok: boolean; out: string }
    if (!r.ok) return void toast.error(r.out)
    if (move) await propose({ file, op: "remove", path: ["permissions", kind], value: rule })
    else {
      toast.success(t("config.main.copiedToUser"))
      reload()
    }
  }
  if (!ov) return <Skeleton className="m-6 h-40" />

  const label = (id: string) =>
    id === "u"
      ? t("config.main.userFile")
      : t(id[0] === "p" ? "config.main.projectLabel" : "config.main.localLabel", { dir: tilde(ov.projects.find((p) => p.dir === id.slice(2))?.project ?? id.slice(2), home) })
  const scope = fileId === "u" ? "u" : fileId[0] // u : utilisateur, p : projet, l : local
  const dirOf = fileId === "u" ? "" : fileId.slice(2)
  const file = ov.files.find((f) => f.id === fileId)
  const doc = (file?.data ?? undefined) as Record<string, unknown> | undefined
  const fileSelect = (
    <div className="mb-5 flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-sm">
      {t("config.main.location")}
      <Choose
        value={scope}
        onChange={(v) => setFileId(v === "u" ? "u" : v + "/" + (dirOf || ov.projects[0]?.dir || ""))}
        options={[
          ["u", t("config.main.userFile")],
          ...(ov.projects.length ? ([["p", t("config.main.optProject")], ["l", t("config.main.optLocal")]] as const) : []),
        ]}
        label={t("config.main.locationAria")}
        className="min-w-0 max-w-xs"
      />
      {scope !== "u" && (
        <Choose value={dirOf} onChange={(d) => setFileId(scope + "/" + d)} options={ov.projects.map((p) => [p.dir, tilde(p.project, home)] as const)} label={t("config.main.projectDirAria")} className="min-w-0 max-w-md" />
      )}
      {!file && <span className="text-xs text-warning">{t("config.main.toCreate")}</span>}
      <span className="text-xs text-muted-foreground">{t("config.main.applyNote")}</span>
    </div>
  )

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-7xl px-3 py-6 md:px-6">
        <Tabs value={tab || "sante"} onValueChange={onTab}>
          {/* Le soulignement de l'onglet actif (::after, 5 px sous le bouton, même invisible) déborde de la liste : sans ce bas de marge il crée un défilement vertical. */}
          {/* ponytail: fondu fixe, même une fois au bout du défilement ; un vrai indicateur demanderait d'écouter le scroll. */}
          <ScrollArea className="mb-2 w-full [mask-image:linear-gradient(to_right,black_calc(100%-2.5rem),transparent)]">
            <div className="pb-2">
              <TabsList className="w-max justify-start">
                <TabsTrigger value="sante">{t("config.main.tabs.health")}{ov.issues.length > 0 && ` (${ov.issues.length})`}</TabsTrigger>
                <TabsTrigger value="projets">{t("config.main.tabs.projects")}</TabsTrigger>
                <TabsTrigger value="precedence">{t("config.main.tabs.precedence")}</TabsTrigger>
                <TabsTrigger value="hooks">{t("config.main.tabs.hooks")} ({ov.hooks.length})</TabsTrigger>
                <TabsTrigger value="mcp">MCP</TabsTrigger>
                <TabsTrigger value="permissions">{t("config.main.tabs.permissions")}</TabsTrigger>
                <TabsTrigger value="reglages">{t("config.main.tabs.settings")}</TabsTrigger>
                <TabsTrigger value="json">JSON</TabsTrigger>
                <TabsTrigger value="sauvegardes">{t("config.main.tabs.backups")} ({ov.backups.length})</TabsTrigger>
                <TabsTrigger value="sync">{t("config.main.tabs.sync")}</TabsTrigger>
              </TabsList>
            </div>
            <ScrollBar orientation="horizontal" />
          </ScrollArea>
          <TabsContent value="sante">
            <HealthTab issues={ov.issues} home={home} projects={ov.projects} />
          </TabsContent>
          <TabsContent value="projets">
            <ProjectsTab
              version={ov.backups.length} // chaque écriture ajoute une sauvegarde : la vue se relit, sans perdre ce qui est déplié
              home={home}
              gone={gone}
              onAddDir={async (file, project, dir) => {
                const p = dir ?? (await a.pickDir(t("config.main.pickDir", { dir: tilde(project, home) }), project))
                if (p) await propose({ file, op: "add", path: ["permissions", "additionalDirectories"], value: p })
              }}
              // Autres dossiers des projets auxquels appartient ce dossier, sans ceux déjà autorisés (~/ et chemins absolus).
              suggest={(project, taken) => {
                const abs = (r: string) => (r.startsWith("~/") ? home + r.slice(1) : r).replace(/\/$/, "")
                const have = new Set(taken.map(abs))
                return [...new Set(a.projects.filter((x) => x.dirs.includes(project)).flatMap((x) => x.dirs))].filter((d) => d !== project && !have.has(d))
              }}
              onRemove={(file, kind, rule) => propose({ file, op: "remove", path: ["permissions", kind], value: rule })}
              onTransfer={transfer}
              onEdit={(file, t) => {
                setFileId(file)
                onTab(t)
              }}
            />
          </TabsContent>
          <TabsContent value="precedence">
            <PrecedenceTab projects={ov.projects} home={home} />
          </TabsContent>
          <TabsContent value="hooks">
            <HooksTab hooks={ov.hooks} />
          </TabsContent>
          <TabsContent value="mcp">
            <McpTab home={home} />
          </TabsContent>
          <TabsContent value="permissions">
            {fileSelect}
            <PermsTab added={added} moveRule={moveRule} gone={gone} onTransfer={transfer} file={fileId} label={label(fileId)} doc={doc} propose={propose} proposePerms={proposePerms} />
          </TabsContent>
          <TabsContent value="reglages">
            {fileSelect}
            <SettingsTab file={fileId} doc={doc} plugins={ov.plugins} propose={propose} />
          </TabsContent>
          <TabsContent value="json">
            {fileSelect}
            <RawTab file={fileId} version={ov.backups.length} proposeRaw={proposeRaw} />
          </TabsContent>
          <TabsContent value="sync">
            <SyncTab />
          </TabsContent>
          <TabsContent value="sauvegardes">
            <BackupsTab backups={ov.backups} onRestored={reload} label={label} />
          </TabsContent>
        </Tabs>
      </div>
      {dialog}
    </ScrollArea>
  )
}
