// swagger-ui-dist ships no types; the API docs page only calls its default export.
declare module 'swagger-ui-dist/swagger-ui-es-bundle.js' {
  const SwaggerUI: (options: Record<string, unknown>) => unknown
  export default SwaggerUI
}
