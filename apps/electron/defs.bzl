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

"""The app Electron runs, for each platform."""

load("@bazel_lib//lib:copy_file.bzl", "copy_file")
load("@bazel_lib//lib:copy_to_directory.bzl", "copy_to_directory")

def electron_app(name, out, programs, exe = "", **kwargs):
    """The app Electron runs (its resources/app): package.json, dist/ (the
    main process and preload), page/ (the desktop page), icon.png and bin/
    with the programs (blitz-tray, which serves the page, and blitzd and
    blitz beside it, where the host looks for them).

    Args:
      name: the target.
      out: the directory it makes, inside this package.
      programs: each program's name in bin/ (without .exe) and its target.
      exe: the programs' suffix (".exe" on Windows).
      **kwargs: common attributes (target_compatible_with, visibility).
    """
    pkg = native.package_name()

    def rel(path):
        # copy_to_directory names a file in this package by its path in it,
        # one elsewhere by its path in the repository.
        return path[len(pkg) + 1:] if path.startswith(pkg + "/") else path

    bins = []
    for prog, label in programs.items():
        copy_file(
            name = name + "_" + prog,
            src = label,
            out = name + "_bin/" + prog + exe,
            is_executable = True,
            **{k: v for k, v in kwargs.items() if k != "visibility"}
        )
        bins.append(":" + name + "_" + prog)
    copy_to_directory(
        name = name,
        srcs = [
            "//apps/electron:appicon.png",
            "//apps/electron:app.package.json",
            "//apps/electron:dist",
            "//apps/electron:page",
        ] + bins,
        out = out,
        replace_prefixes = {
            rel("apps/electron/app.package.json"): "package.json",
            rel("apps/electron/dist"): "dist",
            rel("apps/electron/appicon.png"): "icon.png",
            "apps/desktop/web/dist": "page",
            rel(pkg + "/" + name + "_bin"): "bin",
        },
        **kwargs
    )
