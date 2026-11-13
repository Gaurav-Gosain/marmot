// marmot play's front door. The compile service in the container answers POST /compile
// (marmot play -service); this Worker admits a request only when it comes from an allowed
// origin, is small enough, and its address is under the rate limits, and then hands it to one
// of the container instances.
import { Container, getRandom } from '@cloudflare/containers';

export class PlayService extends Container {
  defaultPort = 8080;
  // An idle instance stops after a while; the image has the standard library compiled, so a
  // new one is warm in seconds.
  sleepAfter = '15m';
}

interface Env {
  PLAY: DurableObjectNamespace<PlayService>;
  PER_MINUTE: RateLimit;
  PER_BURST: RateLimit;
  ALLOWED_ORIGINS: string;
  INSTANCES: string;
  SERVICE_URL?: string;
}

const MAX_BODY = 256 * 1024;

function cors(origin: string | null, env: Env): Record<string, string> {
  const allowed = env.ALLOWED_ORIGINS.split(',').map((s) => s.trim()).filter(Boolean);
  if (!origin || !allowed.includes(origin)) return {};
  return {
    'Access-Control-Allow-Origin': origin,
    'Access-Control-Allow-Methods': 'POST, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
    'Access-Control-Max-Age': '86400',
    Vary: 'Origin',
  };
}

function json(status: number, body: unknown, headers: Record<string, string>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);
    const origin = req.headers.get('Origin');
    const h = cors(origin, env);
    if (url.pathname === '/healthz' && req.method === 'GET') {
      return forward(env, new Request('http://play/healthz'), h);
    }
    if (url.pathname !== '/compile') return json(404, { error: 'not found' }, h);
    if (req.method === 'OPTIONS') return new Response(null, { status: 204, headers: h });
    if (req.method !== 'POST') return json(405, { error: 'POST only' }, h);
    // Browsers send Origin; a page elsewhere may not use the service.
    if (origin && !h['Access-Control-Allow-Origin']) return json(403, { error: 'origin not allowed' }, {});

    const ip = req.headers.get('CF-Connecting-IP') ?? 'local';
    for (const [limit, wait] of [
      [env.PER_BURST, 10],
      [env.PER_MINUTE, 60],
    ] as const) {
      const { success } = await limit.limit({ key: ip });
      if (!success) {
        return json(429, { error: 'too many compiles from your address; wait a little' }, { ...h, 'Retry-After': String(wait) });
      }
    }
    const size = Number(req.headers.get('Content-Length') ?? '0');
    if (size > MAX_BODY) return json(413, { error: 'the program is too large' }, h);
    const body = await req.text();
    if (body.length > MAX_BODY) return json(413, { error: 'the program is too large' }, h);
    return forward(env, new Request('http://play/compile', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body }), h);
  },
} satisfies ExportedHandler<Env>;

async function forward(env: Env, req: Request, h: Record<string, string>): Promise<Response> {
  let res: Response;
  try {
    if (env.SERVICE_URL) {
      const u = new URL(req.url);
      res = await fetch(env.SERVICE_URL + u.pathname, req);
    } else {
      const c = await getRandom(env.PLAY, Number(env.INSTANCES) || 1);
      res = await c.fetch(req);
    }
  } catch (err) {
    return json(502, { error: 'the compile service is not available: ' + String(err) }, h);
  }
  const out = new Response(res.body, res);
  for (const [k, v] of Object.entries(h)) out.headers.set(k, v);
  return out;
}
