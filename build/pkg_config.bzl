# Copyright 2026 Retail Cortex
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""System C libraries found with pkg-config, for cgo code that uses them.

rules_go ignores `#cgo pkg-config:` lines, so a Go package that relies on
them (Wails on Linux: GTK and WebKitGTK) gets its flags from here instead.
The repository runs pkg-config on the host when it's fetched, and has
defs.bzl with COPTS and LINKOPTS; on hosts other than Linux they're empty.
This is deliberately not hermetic: the libraries are the system's.
"""

def _impl(rctx):
    copts = []
    linkopts = []
    if "linux" in rctx.os.name:
        pc = rctx.which("pkg-config")
        if not pc:
            fail("pkg-config isn't installed; it finds %s" % " ".join(rctx.attr.modules))
        for what, out in [("--cflags", copts), ("--libs", linkopts)]:
            r = rctx.execute([pc, what] + rctx.attr.modules)
            if r.return_code != 0:
                fail("pkg-config %s %s: %s" % (what, " ".join(rctx.attr.modules), r.stderr))
            out.extend([f for f in r.stdout.strip().split(" ") if f])
    rctx.file("BUILD.bazel", 'exports_files(["defs.bzl"])\n')
    rctx.file("defs.bzl", "COPTS = %r\nLINKOPTS = %r\n" % (copts, linkopts))

pkg_config_repository = repository_rule(
    implementation = _impl,
    attrs = {"modules": attr.string_list(mandatory = True)},
    environ = ["PKG_CONFIG_PATH"],
    local = True,
)

def _ext(mctx):
    for mod in mctx.modules:
        for lib in mod.tags.library:
            pkg_config_repository(name = lib.name, modules = lib.modules)

pkg_config = module_extension(
    implementation = _ext,
    tag_classes = {"library": tag_class(attrs = {
        "name": attr.string(mandatory = True),
        "modules": attr.string_list(mandatory = True),
    })},
)
