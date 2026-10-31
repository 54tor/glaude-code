import { useEffect, useRef } from "react"
import { Markdown } from "@/components/Message"
import { SCROLL_TOLERANCE_PX, type ScrollLink } from "./scrollLink"

export function MarkdownPreview({ content, link }: { content: string; link?: ScrollLink }) {
  const box = useRef<HTMLDivElement>(null)
  const ratioOf = (element: HTMLElement) => element.scrollTop / Math.max(1, element.scrollHeight - element.clientHeight)
  useEffect(() => {
    const element = box.current
    if (!link || !element) return
    return link.attachPreview((ratio) => {
      const target = ratio * Math.max(1, element.scrollHeight - element.clientHeight)
      if (Math.abs(element.scrollTop - target) > SCROLL_TOLERANCE_PX) element.scrollTop = target
    })
  }, [link])
  return (
    <div ref={box} className="min-w-0 flex-1 overflow-auto border-l p-4" onScroll={(event) => link?.fromPreview(ratioOf(event.currentTarget))}>
      <Markdown t={content} />
    </div>
  )
}
