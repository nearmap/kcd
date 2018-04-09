package cv

const cvListHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>CVManager: Version list</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style id="intercom-stylesheet" type="text/css">
    body,h1{margin:0}body{padding:0}h1,p,table{font-size:13px;font-family:OpenSans,Lucida Grande,Lucida Sans Unicode,sans-serif;font-weight:100;width:100%;color:#555}h1{font-size:16px;width:100%;border-bottom:solid 1px #eee;color:#7d7d7d;padding:3px}td,th{text-align:left;padding-bottom:3px}table{margin:15px}
    </style>
</head>
<body>
<main>
    <h1>List of current version of CV managed workloads</h1>
    <section>
        <div>
            <table >
                <thead>
                <tr>
                    <th scope="col">Namespace</th>
                    <th scope="col">Name</th>
                    <th scope="col">Type</th>
                    <th scope="col">Container</th>
                    <th scope="col">Version</th>
                    <th scope="col">Available pods/Status</th>
                </tr>
                </thead>
                <tbody>
                {{range .}}
                <tr>
                    <td>{{.Namespace}}</td>
                    <td>{{.Name}}</td>
                    <td>{{.Type}}</td>
                    <td>{{.Container}}</td>
                    <td>{{.Version}}</td>
                    <td>{{.AvailablePods}}</td>
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
