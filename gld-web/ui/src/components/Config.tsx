import { useCallback, useEffect, useState } from "react"
import { api, type CfgOverview } from "@/lib/api"
import { tilde } from "@/lib/format"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useActions } from "./actions"
import { Choose } from "./Choose"
import { useConfigEdit } from "./configEdit"
import { HealthTab, HooksTab, McpTab, PrecedenceTab } from "./ConfigTabs1"
import { ProjectsTab } from "./ConfigProjects"
import { RawTab } from "./ConfigRaw"
import { BackupsTab, PermsTab, SettingsTab } from "./ConfigTabs2"

// Configuration de Claude Code : réglages utilisateur, de projet et locaux. Lecture (précédence, santé, hooks, MCP) et
// modifications limitées aux réglages simples, variables d'environnement, plugins et permissions, avec aperçu et sauvegarde.
export function Config({ home, tab, onTab }: { home: string; tab: string; onTab: (t: string) => void }) {
  const [ov, setOv] = useState<CfgOverview | null>(null)
  const [fileId, setFileId] = useState("u") // fichier édité dans « Permissions » et « Réglages »
  const reload = useCallback(() => api.config().then(setOv), [])
  useEffect(() => {
    reload()
  }, [reload])
  const sessions = useActions().live.size // sessions ouvertes : elles rechargent seules les réglages modifiés
  const { propose, proposeRaw, proposePerms, dialog } = useConfigEdit(reload, sessions)
  if (!ov) return <Skeleton className="m-6 h-40" />

  const label = (id: string) =>
    id === "u"
      ? "Utilisateur · ~/.claude/settings.json"
      : `${id[0] === "p" ? "Projet" : "Local"} · ${tilde(ov.projects.find((p) => p.dir === id.slice(2))?.project ?? id.slice(2), home)}`
  const files = ov.files.filter((f) => !f.error)
  // Un projet sans fichier de réglages peut en recevoir un : on propose aussi ses fichiers « local » absents.
  const options: (readonly [string, string])[] = [
    ...files.map((f) => [f.id, label(f.id)] as const),
    ...ov.projects.flatMap((p) => ["p/", "l/"].map((x) => x + p.dir).filter((id) => !ov.files.some((f) => f.id === id)).map((id) => [id, label(id) + " (à créer)"] as const)),
  ]
  const file = ov.files.find((f) => f.id === fileId)
  const doc = (file?.data ?? undefined) as Record<string, unknown> | undefined
  const fileSelect = (
    <div className="mb-4 flex flex-wrap items-center gap-2 text-sm">
      Fichier modifié :
      <Choose value={fileId} onChange={setFileId} options={options} label="Fichier de réglages" className="min-w-0 max-w-xl" />
      <span className="text-xs text-muted-foreground">Chaque modification montre ses différences et fait une sauvegarde. Les sessions ouvertes rechargent seules les réglages (sauf model, effort et plugins : /model, /effort, /reload-plugins).</span>
    </div>
  )

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-5xl px-6 py-6">
        <Tabs value={tab || "sante"} onValueChange={onTab}>
          <TabsList className="mb-4 flex-wrap">
            <TabsTrigger value="sante">Santé{ov.issues.length > 0 && ` (${ov.issues.length})`}</TabsTrigger>
            <TabsTrigger value="projets">Projets</TabsTrigger>
            <TabsTrigger value="precedence">Précédence</TabsTrigger>
            <TabsTrigger value="hooks">Hooks ({ov.hooks.length})</TabsTrigger>
            <TabsTrigger value="mcp">MCP</TabsTrigger>
            <TabsTrigger value="permissions">Permissions</TabsTrigger>
            <TabsTrigger value="reglages">Réglages</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
            <TabsTrigger value="sauvegardes">Sauvegardes ({ov.backups.length})</TabsTrigger>
          </TabsList>
          <TabsContent value="sante">
            <HealthTab issues={ov.issues} home={home} projects={ov.projects} />
          </TabsContent>
          <TabsContent value="projets">
            <ProjectsTab
              version={ov.backups.length} // chaque écriture ajoute une sauvegarde : la vue se relit, sans perdre ce qui est déplié
              home={home}
              onRemove={(file, kind, rule) => propose({ file, op: "remove", path: ["permissions", kind], value: rule })}
              onTransfer={(file, kind, rule, move) =>
                proposePerms("u", JSON.stringify({ permissions: { [kind]: [rule] } }), "merge", move ? () => propose({ file, op: "remove", path: ["permissions", kind], value: rule }) : undefined)
              }
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
            <PermsTab file={fileId} label={label(fileId)} doc={doc} propose={propose} proposePerms={proposePerms} />
          </TabsContent>
          <TabsContent value="reglages">
            {fileSelect}
            <SettingsTab file={fileId} doc={doc} plugins={ov.plugins} propose={propose} />
          </TabsContent>
          <TabsContent value="json">
            {fileSelect}
            <RawTab file={fileId} version={ov.backups.length} proposeRaw={proposeRaw} />
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
