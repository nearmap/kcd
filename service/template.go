package kcd

const kcdListHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>kcd: Version list</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style id="intercom-stylesheet" type="text/css">
    body,h1{margin:0}body{padding:0}h1,p,table{font-size:16px;font-family:OpenSans,Lucida Grande,Lucida Sans Unicode,sans-serif;font-weight:100;width:100%;color:#555;padding:0px;}h1{font-size:18px;width:100%;border-bottom:solid 1px #eee;color:#7d7d7d;padding:3px}td,th{text-align:left;padding-bottom:3px}table{margin:15px}
    </style>
    <style type="text/css">
    p.progress{color:#0066cc;margin:0}
    </style>
    <style type="text/css">
    p.failed{color:#ff0000;margin:0}
    </style>
    <style type="text/css">
    p.success{color:#009933;margin:0}
    </style>
</head>
<body>
<main>
    <h1>KCD managed resources</h1>
    <section>
        <div>
            <table >
                <thead>
                <tr>
                    <th scope="col">Namespace</th>
                    <th scope="col">Name</th>
                    <th scope="col">Container</th>
                    <th scope="col">Status</th>
                    <th scope="col">Current Version</th>
                    <th scope="col">Live Version</th>
                </tr>
                </thead>
                <tbody>
                {{range .}}
                <tr>
                    <td>{{.Namespace}}</td>
                    <td>{{.Name}}</td>
                    <td>{{.Container}}</td>
                    <td><p {{if eq .Status "Failed"}}class="failed"{{else if eq .Status "Progressing"}}class="progress"{{else if and (eq .Status "Success") .Recent}}class="success"{{end}}>{{.Status}}</td>
                    <td>{{.CurrVersion}}</td>
                    <td>{{.LiveVersion}}</td>
                </tr>
                {{end}}
                </tbody>
            </table>
        </div>
    </section>
</main>
</body>
</html>

`
