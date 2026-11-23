import { beforeEach, describe, expect, it } from "vitest"
import type { BgTask } from "./api"
import {
  clearTasks,
  getTaskChips,
  publishTasks,
  resetTasks,
} from "./tasksStore"

const task = (id: string, status = "running"): BgTask => ({
  id,
  desc: `tâche ${id}`,
  status,
  hasOutput: false,
})

describe("tasksStore", () => {
  beforeEach(() => resetTasks())

  it("ne garde que les tâches en cours", () => {
    publishTasks("s1", "/d", [task("a"), task("b", "completed")])
    expect(getTaskChips().map((chip) => chip.task.id)).toEqual(["a"])
  })

  it("publier une session remplace ses tâches sans toucher aux autres", () => {
    publishTasks("s1", "/d", [task("a")])
    publishTasks("s2", "/d", [task("b")])
    publishTasks("s1", "/d", [task("c")])
    expect(getTaskChips().map((chip) => chip.key)).toEqual(["s1:c", "s2:b"])
  })

  it("une tâche qui passe à completed quitte la barre", () => {
    publishTasks("s1", "/d", [task("a")])
    publishTasks("s1", "/d", [task("a", "completed")])
    expect(getTaskChips()).toEqual([])
  })

  it("clearTasks retire la session", () => {
    publishTasks("s1", "/d", [task("a")])
    clearTasks("s1")
    expect(getTaskChips()).toEqual([])
  })

  it("garde la référence du snapshot quand rien ne change", () => {
    publishTasks("s1", "/d", [task("a")])
    const before = getTaskChips()
    publishTasks("s1", "/d", [task("a")])
    expect(getTaskChips()).toBe(before)
  })
})
