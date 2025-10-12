$p = Start-Process -FilePath go -ArgumentList @('run','.') -NoNewWindow -PassThru -RedirectStandardOutput 'server_run.log' -RedirectStandardError 'server_run.err'
Start-Sleep -Seconds 5
try { Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:6060/debug/pprof/heap' -OutFile 'heap1.pb' } catch { Write-Host $_ }
Start-Sleep -Seconds 15
try { Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:6060/debug/pprof/heap' -OutFile 'heap2.pb' } catch { Write-Host $_ }
try { Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=2' -OutFile 'goroutines.txt' } catch { Write-Host $_ }
Stop-Process -Id $p.Id -Force
