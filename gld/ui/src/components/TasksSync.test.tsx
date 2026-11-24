// @vitest-environment jsdom
import { render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { getTaskChips, resetTasks } from "@/lib/tasksStore"
import { TasksSync } from "./TasksSync"

const runningTask = {
  id: "t1",
  desc: "build",
  status: "running",
  hasOutput: false,
}

describe("TasksSync", () => {
  beforeEach(() => {
    resetTasks()
    vi.spyOn(api, "sessionLive").mockResolvedValue({
      tasks: [runningTask],
    } as never)
  })
  afterEach(() => vi.restoreAllMocks())

  it("publie les tâches des sessions actives puis les retire au démontage", async () => {
    const { unmount } = render(
      <TasksSync sessions={[{ id: "s1", dir: "/home/bob/p" }]} />
    )
    await waitFor(() =>
      expect(getTaskChips().map((chip) => chip.key)).toEqual(["s1:t1"])
    )
    unmount()
    expect(getTaskChips()).toEqual([])
  })
})
