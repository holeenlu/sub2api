import subprocess,os,re,json,time
from pathlib import Path
out=Path('/tmp/new-api-resource-matrix');out.mkdir(exist_ok=True)
rows=[]
for version in ['baseline','current']:
 binary=f'/tmp/new-api-body-{version}.test'
 for path in ['passthrough','conversion']:
  for kind in ['text','image']:
   for size in [1,8,32]:
    for concurrency in [1,16]:
     for retries in [0,1]:
      name=f'{version}-{path}-{kind}-{size}MiB-c{concurrency}-r{retries}'
      env=os.environ.copy();env.update(SUB2API_RESOURCE_BENCH='1',BODY_MIB=str(size),BODY_CONCURRENCY=str(concurrency),BODY_RETRIES=str(retries),BODY_KIND=kind,BODY_PATH=path,GOMAXPROCS='16')
      result=subprocess.run(['/usr/bin/time','-l',binary,'-test.run=^$','-test.bench=^BenchmarkGatewayBodyReplay$','-test.benchtime=1x','-test.count=1'],env=env,capture_output=True,text=True,timeout=180)
      log=result.stdout+result.stderr;(out/(name+'.log')).write_text(log)
      if result.returncode: raise RuntimeError(name+' failed: '+log[-1000:])
      line=next(l for l in result.stdout.splitlines() if l.startswith('BenchmarkGatewayBodyReplay'))
      metrics={unit:float(value) for value,unit in re.findall(r'([\d.+-]+)\s+(ns/op|B/op|allocs/op|fd_delta|goroutines_delta|local_first_frame_p50_ms|local_first_frame_p95_ms|requests/op)',line)}
      rss=re.search(r'(\d+)\s+maximum resident set size',log);metrics['peak_rss_bytes']=int(rss.group(1)) if rss else None
      rows.append(dict(version=version,path=path,kind=kind,size_mib=size,concurrency=concurrency,retries=retries,**metrics))
      (out/'results.json').write_text(json.dumps(rows,indent=2))
      print(name+' done',flush=True)
print('COMPLETE',len(rows),flush=True)
