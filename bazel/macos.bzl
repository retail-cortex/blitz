"""macOS code signing for Bazel-built programs.

rules_go links through a temporary file, so the linker's automatic ad hoc
signature names every Go program "a.out": macOS then takes them for one
program (the system log even shows the desktop app under another test's
name), and services that check who's asking, such as the open-panel
service behind the folder dialog, turn it away. These rules re-sign ad
hoc with a real identifier. Elsewhere they only copy.
"""

def _signed_binary_impl(ctx):
    src = ctx.executable.binary
    out = ctx.actions.declare_file(ctx.label.name)
    if ctx.attr.sign:
        cmd = 'cp "$1" "$2" && chmod u+w "$2" && codesign --force --sign - --identifier "$3" "$2"'
    else:
        cmd = 'cp "$1" "$2"'
    ctx.actions.run_shell(
        inputs = [src],
        outputs = [out],
        command = cmd,
        arguments = [src.path, out.path, ctx.attr.identifier],
        mnemonic = "SignBinary",
        progress_message = "Signing %{label}",
    )
    runfiles = ctx.runfiles(files = [out]).merge(ctx.attr.binary[DefaultInfo].default_runfiles)
    return [DefaultInfo(executable = out, files = depset([out]), runfiles = runfiles)]

_signed_binary = rule(
    implementation = _signed_binary_impl,
    attrs = {
        "binary": attr.label(mandatory = True, executable = True, cfg = "target"),
        "identifier": attr.string(mandatory = True),
        "sign": attr.bool(default = False),
    },
    executable = True,
)

def signed_binary(name, binary, identifier, **kwargs):
    """binary, ad hoc signed as identifier on macOS; runnable, with its runfiles."""
    _signed_binary(
        name = name,
        binary = binary,
        identifier = identifier,
        sign = select({
            "@platforms//os:macos": True,
            "//conditions:default": False,
        }),
        **kwargs
    )

def _signed_app_impl(ctx):
    out = ctx.actions.declare_directory(ctx.label.name)
    ctx.actions.run_shell(
        inputs = [ctx.file.app],
        outputs = [out],
        # The bundle's executable takes the bundle's identifier (Info.plist);
        # the programs beside it are signed first, with their own.
        command = 'cp -RL "$1/" "$2" && chmod -R u+w "$2" && codesign --force --sign - --identifier dev.blitz.cli "$2/Contents/MacOS/blitz" && codesign --force --sign - --identifier dev.blitz.service "$2/Contents/MacOS/blitzd" && codesign --force --sign - "$2"',
        arguments = [ctx.file.app.path, out.path],
        mnemonic = "SignApp",
        progress_message = "Signing %{label}",
    )
    return [DefaultInfo(files = depset([out]))]

signed_app = rule(
    implementation = _signed_app_impl,
    doc = "An app bundle (a directory), ad hoc signed so macOS knows it by its bundle identifier.",
    attrs = {"app": attr.label(mandatory = True, allow_single_file = True)},
)
