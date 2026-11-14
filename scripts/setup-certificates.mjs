import fs from 'node:fs/promises';
import https from 'node:https';
import { X509Certificate } from 'node:crypto';

// Source linked by the official GigaChat certificate documentation.
const source = 'https://gu-st.ru/content/lending/russian_trusted_root_ca_pem.crt';
const pem = await new Promise((resolve, reject) => {
  const req = https.get(source, { timeout: 30000 }, res => {
    if (res.statusCode !== 200) { res.resume(); reject(new Error(`CA download HTTP ${res.statusCode}`)); return; }
    let body = '';
    res.on('data', chunk => { body += chunk; if (body.length > 100000) req.destroy(new Error('CA response too large')); });
    res.on('end', () => resolve(body));
  });
  req.on('error', reject);
  req.on('timeout', () => req.destroy(new Error('CA download timeout')));
});
const cert = new X509Certificate(pem);
if (!cert.ca || new Date(cert.validTo) <= new Date() || new Date(cert.validFrom) > new Date()) throw new Error('Downloaded CA is not currently valid');
await fs.mkdir('certs', { recursive: true });
await fs.writeFile('certs/gigachat-ca.pem', pem);
console.log(`CA saved to certs/gigachat-ca.pem. SHA-256: ${cert.fingerprint256}`);
