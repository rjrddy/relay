import {readFileSync,writeFileSync,mkdirSync,copyFileSync} from 'node:fs';
const out='deploy/showcase/dist'; mkdirSync(out,{recursive:true});
for(const name of ['index.html','style.css','app.js'])copyFileSync('deploy/showcase/'+name,out+'/'+name);
const read=name=>JSON.parse(readFileSync('docs/results/'+name,'utf8'));
const reconciliation=read('chaos-hour-reconciliation.json');
writeFileSync(out+'/proof.json',JSON.stringify({campaign:read('chaos-hour.json'),audit:read('independent-audit.json'),tests:read('tests.json'),loads:[100,500,1000].map(n=>read(`load-${n}.json`)),rows:reconciliation.latencies.slice(0,80),export:read('hour-export-status.json')}));
for(const name of ['demo.webm','console-desktop.png','chaos-hour.json','hour-export-status.json'])copyFileSync('docs/results/'+name,out+'/'+name);
console.log('Built evidence site from saved measurements; live engine remains unhosted.');
