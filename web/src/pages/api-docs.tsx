import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { Logo, Stripe, ThemeToggle } from '../ui'

/**
 * The API's description (/api/openapi.yaml) in Swagger UI. Served by the app
 * itself, so "Try it out" calls this server: with your session when signed
 * in, or with an API token entered under Authorize. Swagger UI is large, so it
 * loads only here.
 */
export function ApiDocsPage() {
  const ref = useRef<HTMLDivElement>(null)
  const [failed, setFailed] = useState<string | null>(null)
  useEffect(() => {
    let live = true
    // The self-contained bundle carries its own React: swagger-ui's module build renders with
    // ReactDOM.render, which this app's React 19 no longer has.
    Promise.all([import('swagger-ui-dist/swagger-ui-es-bundle.js'), import('swagger-ui-dist/swagger-ui.css')])
      .then(([mod]) => {
        if (!live || !ref.current) return
        // Depending on the bundler, its function is the default export or nested once more.
        const m = mod as unknown as { default?: unknown }
        const SwaggerUI = [m.default, (m.default as { default?: unknown } | undefined)?.default, m].find(
          (f): f is (o: Record<string, unknown>) => unknown => typeof f === 'function',
        )
        if (!SwaggerUI) throw new Error('swagger-ui did not load')
        SwaggerUI({
          domNode: ref.current,
          url: '/api/openapi.yaml',
          docExpansion: 'list', // areas open, operations folded
          defaultModelsExpandDepth: 0,
          tryItOutEnabled: false,
          // Requests go to this server; the browser adds the session cookie on its own.
          withCredentials: true,
        })
      })
      .catch((e: unknown) => {
        if (live) setFailed(e instanceof Error ? e.message : String(e))
      })
    return () => {
      live = false
    }
  }, [])
  return (
    <>
      <header className="topnav">
        <Logo />
        <Link to="/" className="navlink">
          Back to the app
        </Link>
        <div className="flex-1" />
        <ThemeToggle />
      </header>
      <Stripe />
      <main className="api-docs" aria-label="API documentation">
        <p className="api-docs-note">
          Every request below goes to this server. Signed in, "Try it out" acts as you; a script uses an API token instead
          (Authorize → bearerToken), made by an admin in a workspace's Settings → API tokens.
        </p>
        {failed && <p role="alert">Could not load the API documentation: {failed}</p>}
        <div ref={ref} />
      </main>
    </>
  )
}
