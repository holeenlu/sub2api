interface WeixinJSBridgeLike {
  invoke(action: string, payload: Record<string, unknown>, callback: (result: Record<string, unknown>) => void): void
}
function bridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}
export async function invokeWechatJsapiPayment(payload: Record<string, unknown>, signal: AbortSignal): Promise<Record<string, unknown>> {
  signal.throwIfAborted()
  const api = bridge() ?? await new Promise<WeixinJSBridgeLike | null>((resolve, reject) => {
    function cleanup(): void { clearTimeout(timer); document.removeEventListener('WeixinJSBridgeReady', ready); signal.removeEventListener('abort', abort) }
    function ready(): void { cleanup(); resolve(bridge() ?? null) }
    function abort(): void { cleanup(); reject(new DOMException('Payment context changed', 'AbortError')) }
    const timer = setTimeout(() => { cleanup(); resolve(null) }, 4000)
    document.addEventListener('WeixinJSBridgeReady', ready, { once: true })
    signal.addEventListener('abort', abort, { once: true })
  })
  signal.throwIfAborted()
  if (!api) throw new Error('WeixinJSBridge is unavailable')
  return new Promise((resolve, reject) => {
    function abort(): void { reject(new DOMException('Payment context changed', 'AbortError')) }
    signal.addEventListener('abort', abort, { once: true })
    try {
      api.invoke('getBrandWCPayRequest', payload, result => {
        signal.removeEventListener('abort', abort)
        if (signal.aborted) reject(new DOMException('Payment context changed', 'AbortError'))
        else resolve(result)
      })
    } catch (error) { signal.removeEventListener('abort', abort); reject(error) }
  })
}
