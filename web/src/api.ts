/**
 * Centralized API helper for VIB.
 * Automatically attaches the X-VIB-CSRF-Protection header to all requests
 * ensuring seamless CSRF compliance with the hardened backend API.
 */
export async function apiFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const headers = new Headers(init?.headers || {})
  headers.set('X-VIB-CSRF-Protection', '1')

  return fetch(input, {
    ...init,
    headers,
  })
}
