import fs from 'node:fs';
import https from 'node:https';
import tls from 'node:tls';
import { randomUUID } from 'node:crypto';

const key = process.env.GIGACHAT_AUTHORIZATION_KEY;
if (!key) throw new Error('Configure GIGACHAT_AUTHORIZATION_KEY locally.');
const caPath = process.env.GIGACHAT_CA_FILE;
const agent = new https.Agent({ ca: caPath ? [...tls.rootCertificates, fs.readFileSync(caPath)] : undefined, minVersion: 'TLSv1.2' });
function json(url, options, body) {
  return new Promise((resolve, reject) => {
    const req = https.request(url, { ...options, agent, timeout: 30000 }, res => {
      let data = '';
      res.on('data', chunk => { data += chunk; if (data.length > 1048576) req.destroy(new Error('Response too large')); });
      res.on('end', () => {
        if (res.statusCode !== 200) return reject(new Error(`GigaChat HTTP ${res.statusCode}`));
        try { resolve(JSON.parse(data)); } catch { reject(new Error('Invalid JSON response')); }
      });
    });
    req.on('timeout', () => req.destroy(new Error('GigaChat timeout')));
    req.on('error', reject);
    req.end(body);
  });
}
try {
  const token = await json(process.env.GIGACHAT_OAUTH_URL || 'https://ngw.devices.sberbank.ru:9443/api/v2/oauth', {
    method: 'POST', headers: { Authorization: `Basic ${key}`, RqUID: randomUUID(), 'Content-Type': 'application/x-www-form-urlencoded' },
  }, new URLSearchParams({ scope: process.env.GIGACHAT_SCOPE || 'GIGACHAT_API_PERS' }).toString());
  const models = await json((process.env.GIGACHAT_API_URL || 'https://api.giga.chat/v1').replace(/\/$/, '') + '/models', { headers: { Authorization: `Bearer ${token.access_token}` } });
  console.log(JSON.stringify({ oauth: 'ok', models: models.data.map(model => model.id) }, null, 2));
} catch (error) {
  console.error(`Connection failed: ${error.code || error.message}`);
  process.exitCode = 1;
} finally { agent.destroy(); }
