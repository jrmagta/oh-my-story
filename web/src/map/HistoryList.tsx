import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api, type MapView } from '../api'
import { ErrorAlert, Loading } from '../ui'
import { ago, describe, type HistoryEntry, type HistoryPage } from './history'

/**
 * A history feed, newest first, a page at a time. onStory, when given, makes
 * entries about a story link to it.
 */
export function HistoryList(props: {
  slug: string
  view: MapView
  queryKey: readonly unknown[]
  page: (before?: number) => Promise<HistoryPage>
  label: string
  onStory?: (id: string) => void
}) {
  const members = useQuery({ queryKey: ['ws', props.slug, 'members'], queryFn: () => api.members(props.slug) })
  const pages = useInfiniteQuery({
    queryKey: props.queryKey,
    queryFn: ({ pageParam }) => props.page(pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.next ?? undefined,
  })
  if (pages.isPending) return <Loading />
  if (pages.isError) return <ErrorAlert error={pages.error} />
  const names = { view: props.view, members: members.data ?? [] }
  const rows = pages.data.pages
    .flatMap((p) => p.entries)
    .map((e) => ({ e, text: describe(e, names) }))
    .filter((r): r is { e: HistoryEntry; text: string } => r.text !== null)
  return (
    <div>
      {rows.length === 0 ? (
        <p className="text-muted px-5 py-4 font-medium">Nothing yet.</p>
      ) : (
        <ol aria-label={props.label}>
          {rows.map(({ e, text }) => (
            <li key={e.id} className="border-line border-b px-5 py-3">
              <p className="font-medium">
                <b className="font-extrabold">{e.actorName || 'Someone'}</b> {text}
              </p>
              <p className="text-muted mt-0.5 flex gap-3 text-[12px] font-semibold">
                <time dateTime={e.at} title={new Date(e.at).toLocaleString()}>
                  {ago(e.at)}
                </time>
                {props.onStory && storyOf(e, props.view) && (
                  <button type="button" className="underline" onClick={() => props.onStory?.(storyOf(e, props.view) ?? '')}>
                    Open story
                  </button>
                )}
              </p>
            </li>
          ))}
        </ol>
      )}
      {pages.hasNextPage && (
        <div className="p-5">
          <button type="button" className="btn btn-sm btn-ol w-full" disabled={pages.isFetchingNextPage} onClick={() => pages.fetchNextPage()}>
            Show older
          </button>
        </div>
      )}
    </div>
  )
}

/** The live story an entry is about, if any. */
function storyOf(e: HistoryEntry, view: MapView): string | null {
  const id = e.entity === 'story' ? e.entityId : e.entity === 'placement' ? view.placements.find((p) => p.id === e.entityId)?.storyId : null
  return id && view.stories.some((s) => s.id === id) ? id : null
}
