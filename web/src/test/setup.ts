import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { server } from './server'
import { resetCelebrations } from '@/features/path/celebrate'

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
afterEach(() => {
  cleanup()
  resetCelebrations()
  server.resetHandlers()
})
afterAll(() => server.close())

// jsdom: относительные URL для fetch
if (typeof window !== 'undefined' && !window.location.origin.startsWith('http')) {
  // noop — vitest jsdom задаёт http://localhost:3000
}
