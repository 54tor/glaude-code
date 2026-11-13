import { splitContextTables } from "@/lib/boxTable"

const BLOCK_CLASS = "mb-2 max-h-60 overflow-auto rounded-md border bg-background/70 p-2 font-mono text-xs"

export function QuestionContext({ context }: { context: string }) {
  const segments = splitContextTables(context)
  return (
    <div className={`${BLOCK_CLASS} space-y-2`}>
      {segments.map((segment, segmentIndex) =>
        segment.kind === "text" ? (
          <pre key={segmentIndex} className="break-words whitespace-pre-wrap">{segment.text}</pre>
        ) : (
          <table key={segmentIndex} className="w-full border-collapse text-left">
            <tbody>
              {segment.rows.map((cells, rowIndex) => (
                <tr key={rowIndex} className="border-b last:border-b-0">
                  {cells.map((cellText, cellIndex) => {
                    const CellTag = rowIndex === 0 ? "th" : "td"
                    return (
                      <CellTag key={cellIndex} className="border-r px-2 py-1 align-top font-normal break-words last:border-r-0">
                        {cellText}
                      </CellTag>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        ),
      )}
    </div>
  )
}
