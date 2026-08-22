/**
 * Latest guards a stream of overlapping loads.
 *
 * Two mechanisms, because they cover different failures: the AbortController
 * stops an obsolete request from occupying the connection, and the version
 * counter stops a response that was already in flight when the user changed
 * something from overwriting the newer state on arrival.
 */
export class Latest {
  private token = 0
  private controller: AbortController | null = null

  begin(): { id: number; signal: AbortSignal } {
    this.controller?.abort()
    this.controller = new AbortController()
    return { id: ++this.token, signal: this.controller.signal }
  }

  isCurrent(id: number): boolean {
    return id === this.token
  }

  cancel() {
    this.controller?.abort()
    this.controller = null
  }
}
