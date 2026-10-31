import { useCallback, useEffect, useState } from "react"
import { Stethoscope } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api, type CfgOverview } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ClaudeOptions, SettingsPanel, type UiSettings } from "./SettingsDialog"
import { useActions } from "./actions"
import { Choose } from "./Choose"
import { useConfigEdit } from "./configEdit"
import { HealthTab, PrecedenceTab } from "./ConfigTabs1"
import { ProjectsTab } from "./ConfigProjects"
import { RawTab } from "./ConfigRaw"
import { BackupsTab, PermsTab, SettingsTab } from "./ConfigTabs2"
import { SyncTab } from "./ConfigSync"
import { Backup } from "./Backup"
import { takeConfigFile } from "@/lib/wantConfigFile"
import { Button } from "@/components/ui/button"

// Configuration de Claude Code : réglages utilisateur, de projet et locaux. Lecture (précédence, santé, hooks) et
// modifications limitées aux réglages simples, variables d'environnement, plugins et permissions, avec aperçu et sauvegarde.
// dir : écran d'un seul projet (permissions et réglages de ses fichiers partagé et local), avec un lien vers les permissions globales.
export function Config({ home, tab, onTab, ui, dir }: { home: string; tab: string; onTab: (tabName: string) => void; ui: UiSettings; dir?: string }) {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<CfgOverview | null>(null)
  const [fileId, setFileId] = useState(() => (dir ? "p/" + dir : takeConfigFile() || "u"))
  const [projTab, setProjTab] = useState("permissions") // fichier édité dans « Permissions » et « Réglages »
  const reload = useCallback(() => api.config().then(setOverview), [])
  useEffect(() => {
    reload()
  }, [reload])
  const actions = useActions()
  const sessions = actions.live.size // sessions ouvertes : elles rechargent seules les réglages modifiés
  const { gone, added, moveRule, propose, proposeRaw, proposePerms, dialog } = useConfigEdit(reload, sessions)
  // Direct, sans fenêtre : la règle rejoint vos réglages, puis (déplacement) quitte son fichier d'origine.
  const transfer = async (file: string, kind: string, rule: string, move: boolean) => {
    if (kind === "additionalDirectories") {
      // l'import de règles ignore les dossiers : ajout direct
      await propose({ file: "u", op: "add", path: ["permissions", kind], value: rule })
      if (move) await propose({ file, op: "remove", path: ["permissions", kind], value: rule })
      return
    }
    try {
      await api.configPermImport({ file: "u", text: JSON.stringify({ permissions: { [kind]: [rule] } }), mode: "merge", dry: false })
    } catch (error) {
      toast.error(errMsg(error))
      return
    }
    if (move) {
      await propose({ file, op: "remove", path: ["permissions", kind], value: rule })
      return
    }
    toast.success(t("config.main.copiedToUser"))
    reload()
  }
  if (!overview) return <Skeleton className="m-6 h-40" />

  const label = (id: string) => {
    if (id === "u") return t("config.main.userFile")
    const projectDir = id.slice(2)
    const projectPath = overview.projects.find((project) => project.dir === projectDir)?.project ?? projectDir
    const labelKey = id[0] === "p" ? "config.main.projectLabel" : "config.main.localLabel"
    return t(labelKey, { dir: tilde(projectPath, home) })
  }
  const scope = fileId === "u" ? "u" : fileId[0] // u : utilisateur, p : projet, l : local
  const dirOf = fileId === "u" ? "" : fileId.slice(2)
  const file = overview.files.find((configFile) => configFile.id === fileId)
  const doc = (file?.data ?? undefined) as Record<string, unknown> | undefined
  const changeScope = (selectedScope: string) => {
    if (selectedScope === "u") {
      setFileId("u")
      return
    }
    const targetDir = dirOf || dir || overview.projects[0]?.dir || ""
    setFileId(selectedScope + "/" + targetDir)
  }
  const projectScopeOptions = [["p", t("config.main.optProject")], ["l", t("config.main.optLocal")]] as const
  const hasProjects = overview.projects.length > 0
  const scopeOptions = dir ? projectScopeOptions : [["u", t("config.main.userFile")] as const, ...(hasProjects ? projectScopeOptions : [])]
  const fileSelect = (
    <div className="mb-5 flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-sm">
      {t("config.main.location")}
      <Choose
        value={scope}
        onChange={changeScope}
        options={scopeOptions}
        label={t("config.main.locationAria")}
        size="sm" className="min-w-0 max-w-xs"
      />
      {dir && (
        <Button size="sm" variant="outline" className="ml-auto" onClick={() => onTab("permissions")} title={t("config.main.globalPermsHint")}>
          {t("config.main.globalPerms")}
        </Button>
      )}
      {!dir && scope !== "u" && (
        <Choose value={dirOf} onChange={(projectDir) => setFileId(scope + "/" + projectDir)} options={overview.projects.map((project) => [project.dir, tilde(project.project, home)] as const)} label={t("config.main.projectDirAria")} size="sm" className="min-w-0 max-w-md" />
      )}
      {!file && <span className="text-xs text-warning">{t("config.main.toCreate")}</span>}
      <span className="text-xs text-muted-foreground">{t("config.main.applyNote")}</span>
    </div>
  )

  // Trois groupes : ce qu'on édite dans Claude Code, ce qu'on lit pour comprendre, et gld lui-même.
  const issueCountSuffix = overview.issues.length > 0 ? ` (${overview.issues.length})` : ""
  const groups: [string, [string, string][]][] = [
    ["claude", [["reglages", t("config.main.tabs.settings")], ["permissions", t("config.main.tabs.permissions")], ["json", "JSON"], ["sauvegardes", `${t("config.main.tabs.backups")} (${overview.backups.length})`]]],
    ["diagnostic", [["sante", t("config.main.tabs.health") + issueCountSuffix], ["projets", t("config.main.tabs.projects")], ["precedence", t("config.main.tabs.precedence")]]],
    ["gld", [["interface", t("config.main.tabs.interface")], ["export", t("shell.app.title.backup")], ["sync", t("config.main.tabs.sync")]]],
  ]
  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-page px-3 py-6 md:px-6">
        <Tabs value={dir ? projTab : tab || "sante"} onValueChange={dir ? setProjTab : onTab}>
          {dir ? (
            <TabsList className="mb-4 w-max">
              <TabsTrigger value="permissions">{t("config.main.tabs.permissions")}</TabsTrigger>
              <TabsTrigger value="reglages">{t("config.main.tabs.settings")}</TabsTrigger>
            </TabsList>
          ) : (
            <div className="mb-4 flex flex-wrap gap-x-6 gap-y-3">
              {groups.map(([groupName, groupTabs]) => (
                <div key={groupName}>
                  <p className="mb-1 px-1 text-2xs font-medium tracking-wide text-muted-foreground uppercase">{t(`config.main.groups.${groupName}`)}</p>
                  <TabsList className="w-max">
                    {groupTabs.map(([tabValue, tabLabel]) => (
                      <TabsTrigger key={tabValue} value={tabValue}>{tabLabel}</TabsTrigger>
                    ))}
                  </TabsList>
                </div>
              ))}
            </div>
          )}
          <TabsContent value="sante">
            <HealthTab issues={overview.issues} home={home} projects={overview.projects} />
          </TabsContent>
          <TabsContent value="projets">
            <ProjectsTab
              version={overview.backups.length} // chaque écriture ajoute une sauvegarde : la vue se relit, sans perdre ce qui est déplié
              home={home}
              gone={gone}
              onAddDir={async (file, project, dir) => {
                const pickedDir = dir ?? (await actions.pickDir(t("config.main.pickDir", { dir: tilde(project, home) }), project))
                if (pickedDir) await propose({ file, op: "add", path: ["permissions", "additionalDirectories"], value: pickedDir })
              }}
              // Autres dossiers des projets auxquels appartient ce dossier, sans ceux déjà autorisés (~/ et chemins absolus).
              suggest={(project, taken) => {
                const toAbsolutePath = (rule: string) => {
                  const expandedPath = rule.startsWith("~/") ? home + rule.slice(1) : rule
                  return expandedPath.replace(/\/$/, "")
                }
                const alreadyAllowed = new Set(taken.map(toAbsolutePath))
                const relatedDirs = actions.projects.filter((knownProject) => knownProject.dirs.includes(project)).flatMap((knownProject) => knownProject.dirs)
                return [...new Set(relatedDirs)].filter((relatedDir) => relatedDir !== project && !alreadyAllowed.has(relatedDir))
              }}
              onRemove={(file, kind, rule) => propose({ file, op: "remove", path: ["permissions", kind], value: rule })}
              onTransfer={transfer}
              onEdit={(file, targetTab) => {
                setFileId(file)
                onTab(targetTab)
              }}
            />
          </TabsContent>
          <TabsContent value="precedence">
            <PrecedenceTab projects={overview.projects} home={home} />
          </TabsContent>
          <TabsContent value="permissions">
            {fileSelect}
            <PermsTab added={added} moveRule={moveRule} gone={gone} onTransfer={transfer} file={fileId} label={label(fileId)} doc={doc} propose={propose} proposePerms={proposePerms} />
          </TabsContent>
          <TabsContent value="reglages">
            {!dir && (
              <div className="mb-6">
                <Button size="sm" variant="outline" className="mb-3" onClick={() => actions.newSession(overview.projects[0]?.dir ?? home, "/doctor")} title={t("config.main.doctorHint")}>
                  <Stethoscope /> {t("config.main.doctor")}
                </Button>
                <ClaudeOptions args={ui.args} setArgs={ui.setArgs} choice={ui.choice} setChoice={ui.setChoice} />
              </div>
            )}
            {fileSelect}
            <SettingsTab file={fileId} doc={doc} plugins={overview.plugins} propose={propose} />
          </TabsContent>
          <TabsContent value="interface">
            <SettingsPanel font={ui.font} setFont={ui.setFont} fontSize={ui.fontSize} setFontSize={ui.setFontSize} />
          </TabsContent>
          <TabsContent value="json">
            {fileSelect}
            <RawTab file={fileId} version={overview.backups.length} proposeRaw={proposeRaw} />
          </TabsContent>
          <TabsContent value="export">
            <Backup home={home} />
          </TabsContent>
          <TabsContent value="sync">
            <SyncTab />
          </TabsContent>
          <TabsContent value="sauvegardes">
            <BackupsTab backups={overview.backups} onRestored={reload} label={label} />
          </TabsContent>
        </Tabs>
      </div>
      {dialog}
    </ScrollArea>
  )
}
