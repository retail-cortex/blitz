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

"""es_proto_library: TypeScript for a proto_library, with protoc-gen-es."""

load("@protobuf//bazel/common:proto_info.bzl", "ProtoInfo")

_PROTO_TOOLCHAIN = "@protobuf//bazel/private:proto_toolchain_type"

def _impl(ctx):
    protoc = ctx.toolchains[_PROTO_TOOLCHAIN].proto.proto_compiler
    info = ctx.attr.proto[ProtoInfo]
    root = info.proto_source_root
    names = []
    outs = []
    for src in info.direct_sources:
        name = src.path[len(root) + 1:] if root != "." else src.path
        names.append(name)
        outs.append(ctx.actions.declare_file("%s/%s_pb.ts" % (ctx.attr.out_dir, name[:-len(".proto")])))

    args = ctx.actions.args()
    args.add("--plugin=protoc-gen-es=" + ctx.executable.plugin.path)
    args.add("--es_out=%s/%s/%s" % (ctx.bin_dir.path, ctx.label.package, ctx.attr.out_dir))
    args.add("--es_opt=target=ts")
    # From the sources, not descriptor sets, so the output keeps the
    # protos' comments.
    args.add_all(info.transitive_proto_path, before_each = "-I")
    args.add_all(names)
    ctx.actions.run(
        executable = protoc,
        arguments = [args],
        inputs = info.transitive_sources,
        tools = [ctx.attr.plugin[DefaultInfo].files_to_run],
        outputs = outs,
        env = {"BAZEL_BINDIR": ctx.bin_dir.path},
        mnemonic = "EsProto",
        progress_message = "Generating TypeScript for %{label}",
    )
    return [DefaultInfo(files = depset(outs))]

es_proto_library = rule(
    implementation = _impl,
    attrs = {
        "proto": attr.label(mandatory = True, providers = [ProtoInfo]),
        "plugin": attr.label(mandatory = True, executable = True, cfg = "exec"),
        "out_dir": attr.string(default = "gen"),
    },
    toolchains = [_PROTO_TOOLCHAIN],
)
