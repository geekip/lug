# geekip/lug
Package contains is a libs for [gopher-lua](https://github.com/yuin/gopher-lua).

## Features

* [fs](#fs)
* [http](#http)
* [json](#json)
* [router](#router)
* [template](#template)


# Install
``` shell
git clone git@github.com:geekip/lug.git
go build .
```

# Usage
```
$ lug -h

Usage: lug [options] [script [args]]
Available options are:
  -e stat  execute string 'stat'
  -i       enter interactive mode after executing 'script'
  -l name  require library 'name'
  -m MB    memory limit (default: unlimited)
  -dt      dump AST trees
  -dc      dump VM codes
  -p file  write cpu profile to the file
  -v       show version information
```

``` shell
# specify file
lug /code/lua/test.lua foo bar 123
```
``` lua
-- /code/lua/test.lua

for k, v in pairs(arg) do 
  print(k, v) 
end

-- -1 /usr/bin/lug
-- 0  /code/lua/test.lua
-- 1  foo
-- 2  bar
-- 3  123
```

#  Built in Library
### fs
``` lua
local fs = require("fs")

-- fs.mkdir(path, [recursive, mode])
local result = fs.mkdir("/var/tmp/test", true, 0755)
if not result then error("mkdir") end

-- fs.copy(path, dest)
local result = fs.copy("/var/tmp/test.lua", "/var/tmp2/test.lua")
if not result then error("copy") end

-- fs.move(path, dest)
local result = fs.move("/var/tmp/test.lua", "/var/tmp2/test.lua")
if not result then error("move") end

-- fs.remove(path, [recursive])
local result = fs.remove("/var/tmp/test", true)
if not result then error("remove") end

-- fs.read(file)
local result = fs.read("/var/tmp/test", true)
if not(result ~= "test text") then error("read") end

-- fs.write(file, content, [append, mode])
local result = fs.write("/var/tmp/test/test.txt", "test text", false, 644)
if not result then error("write") end

local result = fs.write("/var/tmp/test/test.txt", "test text", true)
if not result then error("write append") end

-- fs.isdir(path)
local result = fs.isdir("/var/tmp/test/test.lua")
if not result then error("isdir") end

-- fs.dirname(path)
local result = fs.dirname("/var/tmp/test/test.lua")
if not(result == "test") then error("dirname") end

-- fs.basename(path)
local result = fs.basename("/var/tmp/test/test.lua")
if not(result == "test.lua") then error("basename") end

-- fs.ext(file)
local result = fs.ext("/var/tmp/test/test.lua")
if not(result == ".lua") then error("ext") end

-- fs.exedir()
local result = fs.exedir()
if not(result == "/usr/bin") then error("exedir") end

-- fs.cwdir()
local result = fs.cwdir()
if not(result == "/root") then error("cwdir") end

-- fs.symlink(target, link)
local result = fs.symlink("/root/lug","/usr/bin/lug")
if not result then error("symlink") end

-- fs.exists(path)
local result = fs.exists("/root/lug")
if not result then error("exists") end

-- fs.glob(pattern)
local result = fs.glob("/var/tmp/*")
if not(result[1] == "/var/tmp/test") then error("glob") end

-- fs.join(elem...)
local result = fs.join("/foo", "bar", "baz")
if not(result == "/foo/bar/baz") then error("join") end

-- fs.clean(path)
local result = fs.clean("/foo/..bar/.baz")
if not(result == "/foo/baz") then error("clean") end

-- fs.abspath(path)
local result = fs.abspath("./test")
if not(result == "/root/test") then error("abspath") end

-- fs.isabs(path)
local result = fs.isabs("/root/test")
if not result then error("isabs") end

```

### http

``` lua
local http = require("http")
local result = http.request("GET","http://www.google.com")
print(result.status)
print(result.headers)
print(result.body)

local result = http.get("http://www.google.com")

local result = http.get("http://www.google.com",{
  timeout = 300,
  headers = {
    ["User-Agent"] = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"
  }
})
```

### json

``` lua
local json = require("json")

-- json.decode()
local jsonString = [[
  {
    "a": {"b":1}
  }
]]

local result, err = json.decode(jsonString)
if err then
  error(err)
end

-- json.encode()
local table = { a = { b = 1 } }
local result, err = json.encode(table)
if err then
  error(err)
end

```

### router

``` lua
local json = require("json")
local router = require("router")

-- connect delete get head options patch post put trace

-- get method
router.handle("GET", "/handle", function(method, path, params)
  return "handle page"
end)

router.get("/", function(method, path, params)
  return {
    status = 200,
    headers = { ["Content-Type"] = "text/html;charset=UTF-8" },
    body = "<h1>Welcome to the Home Page</h1>"
  }
end)

-- post method
router.post("/hello/{id}", function(method, path, params)
  return {
    status = 200,
    headers = { ["Content-Type"] = "application/json" },
    body = json.encode({ id = params['id'] })
  }
end)

-- all method
router.handle("*", "/handle", function(method, path, params)
  return "handle page"
end)

router.all("/handle", function(method, path, params)
  return "handle page"
end)

-- static file server
router.files("/web/{*}", "var/wwwroot/web")
router.file("/js", "/var/wwwroot/web/main.js")

-- run http server
router.listen(":8080")
```


### template

``` lua
local template = require("template")

local html, err = template.file("view.html", { name = "World" })
if err then
  error(err)
else
  print(html)
end

local text, err = template.string("Hello {{.name}}!", {name = "World"})
if err then
  error(err)
else
  print(text)
end
```
