export function extractIdentifier(req) {
  const apiKey = req.headers['x-api-key'];
  if (apiKey) return apiKey;

  const auth = req.headers['authorization'];
  if (auth) return auth.replace(/^Bearer\s+/i, '');

  return req.ip;
}
