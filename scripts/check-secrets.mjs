import fs from 'node:fs';
import { execFileSync } from 'node:child_process';

const files = [...new Set(execFileSync('git', ['ls-files', '-c', '-o', '--exclude-standard', '-z']).toString('utf8').split('\0').filter(Boolean))];
let key = '';
if (fs.existsSync('.env')) {
  key = (fs.readFileSync('.env', 'utf8').match(/^GIGACHAT_AUTHORIZATION_KEY\s*=\s*(.*)$/m)?.[1] || '').trim().replace(/^['"]|['"]$/g, '');
}
const forbidden = files.filter(file => file === '.env' || (file.startsWith('.env.') && !['.env.example', '.env.sample'].includes(file)) || file.startsWith('certs/'));
if (key.length >= 8) {
  const needle = Buffer.from(key);
  for (const file of files) {
    if (file.endsWith('.zip') || !fs.statSync(file).isFile()) continue;
    if (fs.readFileSync(file).includes(needle)) forbidden.push(file);
  }
  const imageName = process.env.ONBO_IMAGE || 'onbo-app';
  const image = execFileSync('docker', ['image', 'inspect', imageName, '--format', '{{json .Config.Env}}']);
  if (image.includes(Buffer.from(key))) forbidden.push('Docker image environment');
}
if (forbidden.length) { console.error('Secret exposure detected in:', [...new Set(forbidden)].join(', ')); process.exitCode = 1; }
else console.log(`Secret check: OK (${files.length} source/artifact files; local environment and CA excluded).`);
