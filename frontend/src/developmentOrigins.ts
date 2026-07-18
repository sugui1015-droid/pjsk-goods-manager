const localViteOrigins = ['http://localhost:5173', 'http://127.0.0.1:5173'] as const

export function localDevelopmentFrontendOrigins(): string[] {
  return [...localViteOrigins]
}
