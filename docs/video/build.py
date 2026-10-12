#!/usr/bin/env python3
"""Builds the narrated, subtitled intro video for the README.

    python3 docs/video/build.py

Every terminal scene is a real recording made with VHS: commands run against
the published module and a real provider. Narration comes from OpenAI
text-to-speech, and subtitles are timed from the generated audio.

Requires: vhs and ttyd, ffmpeg with libass, Go, agent-browser (to render the
cards), the Inter and JetBrains Mono fonts, and OPENAI_API_KEY.

Outputs: docs/assets/intro.mp4, intro.srt, and intro-poster.png.
"""

import hashlib
import json
import os
import re
import shutil
import subprocess
import urllib.request
import wave
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
HERE = REPO / "docs" / "video"
ASSETS = REPO / "docs" / "assets"
CACHE = HERE / ".cache"
WORK = Path(os.environ.get("INTRO_WORK", "/tmp/aesthetics-intro"))
PROJECT = WORK / "report"

W, H, TERM_H, FPS = 1920, 1080, 900, 30
BG = "0x0b0e14"
VOICE = {
    "model": "gpt-4o-mini-tts",
    "voice": "cedar",
    "instructions": "Warm, calm, and confident, like a thoughtful engineer introducing a tool they love. "
    "Unhurried, clear diction, natural pauses.",
}
LEAD, GAP, TAIL = 0.5, 0.35, 0.8  # seconds around and between narration lines
TYPING = 0.045                     # seconds per typed character

PROMPT_SETUP = (
    r"""unset NO_COLOR CLICOLOR CLICOLOR_FORCE; export TERM=xterm-256color """
    r"""PS1='\[\e[38;5;209m\]❯\[\e[0m\] ' COLUMNS=120 GOPROXY=direct GOFLAGS= """
    r"""PATH=$HOME/go/bin:/usr/local/go/bin:$PATH; shopt -u checkwinsize; """
    f"cd {PROJECT}; clear"
)

# Each scene is a card or a terminal recording, with its narration. Terminal
# commands are typed and run for real; "hidden" commands prepare state off camera.
SCENES = [
    {"id": "title", "card": "title", "lines": [
        "Linters check what a program can decide.",
        "Aesthetics checks what takes judgment: clarity, simplicity, and coherence.",
    ]},
    {"id": "install", "commands": [
        "go install github.com/sadasant/aesthetics/cmd/aesthetics@latest",
        "aesthetics version",
    ], "lines": [
        "Install it with a single command.",
        "It's one small binary, for macOS, Linux, and Windows.",
    ]},
    {"id": "init", "commands": [
        "aesthetics init --use unix,kiss",
        "head -22 .aesthetics.yaml",
    ], "lines": [
        "In your project, run aesthetics init, and pick the schools of thought you care about.",
        "Here, the Unix philosophy, and Keep It Simple.",
        "It works with OpenAI, Cloudflare, TypeSafe, or a model you host yourself.",
        "And your own guidelines live right beside them, written in plain language.",
    ]},
    {"id": "check", "commands": [
        "aesthetics check cmd/report/main.go",
    ], "lines": [
        "Then run aesthetics check.",
        "A decision model judges every guideline, and reports pass, fail, or indeterminate.",
        "This entry point does too much. Each failure comes with a recommendation, and the lines that show the problem.",
    ]},
    {"id": "explain", "commands": [
        "aesthetics explain cmd/report/main.go | head -20",
    ], "lines": [
        "To see why a guideline applies, ask aesthetics explain.",
        "It lists every guideline for the file, in order of precedence, and where each one came from.",
    ]},
    {"id": "diff", "hidden": [
        "git add -A && git commit -qm 'first version' && cat ../makeall.go.txt >> internal/slug/slug.go",
    ], "commands": [
        "git diff --stat",
        "aesthetics check --diff --scope file",
    ], "lines": [
        "In CI, check only what changed.",
        "Every failure is marked as introduced by the change, or preexisting, so old problems never block new work.",
    ]},
    {"id": "outro", "card": "outro", "lines": [
        "Aesthetics: make your taste explicit.",
        "It's open source, on GitHub.",
    ]},
]

MAKEALL = """
var Calls int

// MakeAll slugs every title and counts calls.
func MakeAll(titles []string) []string {
\tvar out []string
\tfor _, t := range titles {
\t\tCalls++
\t\tout = append(out, Make(t))
\t}
\treturn out
}
"""

VHS_THEME = {
    "name": "Aesthetics", "background": "#161b22", "foreground": "#e6edf3", "cursor": "#f0883e",
    "selection": "#264f78", "black": "#484f58", "red": "#ff7b72", "green": "#3fb950", "yellow": "#d29922",
    "blue": "#58a6ff", "magenta": "#bc8cff", "cyan": "#39c5cf", "white": "#b1bac4", "brightBlack": "#7d8590",
    "brightRed": "#ffa198", "brightGreen": "#56d364", "brightYellow": "#e3b341", "brightBlue": "#79c0ff",
    "brightMagenta": "#d2a8ff", "brightCyan": "#56d4dd", "brightWhite": "#ffffff",
}


def run(*cmd, **kw):
    print("+", " ".join(str(c) for c in cmd)[:160])
    subprocess.run([str(c) for c in cmd], check=True, **kw)


def duration(path):
    out = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", str(path)],
                         check=True, capture_output=True, text=True).stdout
    return float(out.strip())


def tts(text):
    """Returns a cached WAV file with the narration of text."""
    key = hashlib.sha256(json.dumps([VOICE, text]).encode()).hexdigest()[:20]
    path = CACHE / f"{key}.wav"
    if not path.exists():
        CACHE.mkdir(parents=True, exist_ok=True)
        req = urllib.request.Request(
            "https://api.openai.com/v1/audio/speech",
            data=json.dumps({**VOICE, "input": text, "response_format": "wav"}).encode(),
            headers={"Authorization": "Bearer " + os.environ["OPENAI_API_KEY"], "Content-Type": "application/json"},
        )
        path.write_bytes(urllib.request.urlopen(req).read())
    return path


def pcm_frames(path):
    """Reads PCM data from a WAV file. Streamed WAVs from the speech API carry
    placeholder lengths in their headers, so the data is read to the end."""
    with wave.open(str(path)) as w:
        data = w.readframes(1 << 30)
        width = w.getsampwidth() * w.getnchannels()
    return data[: len(data) - len(data) % width]


def wav_seconds(path):
    with wave.open(str(path)) as w:
        bytes_per_second = w.getframerate() * w.getsampwidth() * w.getnchannels()
    return len(pcm_frames(path)) / bytes_per_second


def setup_project():
    """Creates the demo project from the tutorial's sources."""
    if WORK.exists():
        shutil.rmtree(WORK)
    (PROJECT / "internal" / "slug").mkdir(parents=True)
    (PROJECT / "cmd" / "report").mkdir(parents=True)
    tutorial = (REPO / "docs" / "tutorial.md").read_text()
    for path, code in re.findall(r"Save this as `([^`]+)`:\n\n```go\n(.*?)```", tutorial, re.S):
        (PROJECT / path).write_text(code)
    (WORK / "makeall.go.txt").write_text(MAKEALL)
    git = ["git", "-c", "user.email=demo@example.com", "-c", "user.name=demo"]
    run(*git, "init", "-q", cwd=PROJECT)
    run(*git, "add", "-A", cwd=PROJECT)
    run(*git, "commit", "-qm", "init", cwd=PROJECT)


def render_cards():
    cards = WORK / "cards"
    cards.mkdir(parents=True, exist_ok=True)
    page = (HERE / "card.html").as_uri()
    for kind in ["title", "outro"]:
        screenshot(f"{page}?kind={kind}", W, H, cards / f"{kind}.png")


def screenshot(url, w, h, out):
    run("agent-browser", "set", "viewport", w, h, 1)
    run("agent-browser", "open", url)
    run("agent-browser", "wait", "1500")
    run("agent-browser", "screenshot", out)


def render_poster(joined, at):
    """The README poster: a frame of the check scene with a play button."""
    frame = WORK / "cards" / "frame.png"
    run("ffmpeg", "-y", "-loglevel", "error", "-ss", f"{at:.2f}", "-i", joined, "-frames:v", "1", frame)
    page = (HERE / "card.html").as_uri()
    screenshot(f"{page}?kind=poster&frame={frame.as_uri()}", 1280, 720, ASSETS / "intro-poster.png")


def vhs_quote(s):
    return "`" + s + "`" if '"' in s or "\\" in s else '"' + s + '"'


def record(scene, needed):
    """Records a terminal scene, holding the last frame until the narration fits."""
    theme = json.dumps(VHS_THEME)
    est = 0.6 + sum(len(c) * TYPING + 1.6 for c in scene["commands"])
    hold = max(1.5, needed - est)
    out = WORK / f"{scene['id']}.mp4"
    lines = [
        f'Output "{out}"',
        'Set Shell "bash"',
        'Set FontFamily "JetBrains Mono"',
        "Set FontSize 21",
        "Set LineHeight 1.1",
        f"Set Width {W}",
        f"Set Height {TERM_H}",
        "Set Padding 24",
        "Set Margin 32",
        'Set MarginFill "#0b0e14"',
        "Set BorderRadius 14",
        "Set WindowBar Colorful",
        "Set WindowBarSize 32",
        f"Set Theme {theme}",
        f"Set TypingSpeed {int(TYPING * 1000)}ms",
        f"Set Framerate {FPS}",
        "Set WaitTimeout 90s",
        r"Set WaitPattern /❯\s*$/",
        "Hide",
        f"Type {vhs_quote(PROMPT_SETUP)}",
        "Enter",
        "Wait",
    ]
    for h in scene.get("hidden", []):
        lines += [f"Type {vhs_quote(h)}", "Enter", "Wait", "Type `clear`", "Enter", "Wait"]
    lines += ["Show", "Sleep 600ms"]
    for c in scene["commands"]:
        lines += [f"Type {vhs_quote(c)}", "Sleep 400ms", "Enter", "Wait", "Sleep 1200ms"]
    lines.append(f"Sleep {hold:.1f}s")
    tape = WORK / f"{scene['id']}.tape"
    tape.write_text("\n".join(lines) + "\n")
    run("vhs", tape)
    return out


def ass_time(t):
    cs = int(round(t * 100))
    return f"{cs // 360000}:{cs // 6000 % 60:02}:{cs // 100 % 60:02}.{cs % 100:02}"


def srt_time(t):
    ms = int(round(t * 1000))
    return f"{ms // 3600000:02}:{ms // 60000 % 60:02}:{ms // 1000 % 60:02},{ms % 1000:03}"


def main():
    setup_project()
    render_cards()

    # Narration first: its length sets each scene's length.
    for s in SCENES:
        s["audio"] = [tts(line) for line in s["lines"]]
        s["speech"] = sum(wav_seconds(a) for a in s["audio"]) + GAP * (len(s["lines"]) - 1)
        s["needed"] = LEAD + s["speech"] + TAIL

    clips = []
    for s in SCENES:
        clip = WORK / f"{s['id']}-clip.mp4"
        if "card" in s:
            d = s["needed"] + 0.4
            run("ffmpeg", "-y", "-loglevel", "error", "-loop", "1", "-i", WORK / "cards" / f"{s['card']}.png",
                "-t", f"{d:.3f}", "-r", FPS,
                "-vf", f"scale={W}:{H},fade=in:st=0:d=0.5,fade=out:st={d - 0.5:.3f}:d=0.5,format=yuv420p",
                "-c:v", "libx264", "-crf", "18", clip)
        else:
            raw = record(s, s["needed"])
            d = max(duration(raw), s["needed"])
            run("ffmpeg", "-y", "-loglevel", "error", "-i", raw, "-r", FPS,
                "-vf", f"pad={W}:{H}:0:0:color={BG},tpad=stop_mode=clone:stop_duration=30,"
                       f"trim=duration={d:.3f},format=yuv420p",
                "-c:v", "libx264", "-crf", "18", clip)
        s["duration"] = duration(clip)
        clips.append(clip)

    # Timeline: narration lines and subtitles at their place in each scene.
    with wave.open(str(SCENES[0]["audio"][0])) as w0:
        params = w0.getparams()
    rate, width = params.framerate, params.sampwidth * params.nchannels
    pcm = bytearray()
    cues = []
    start = 0.0
    for s in SCENES:
        t = start + LEAD
        for line, audio in zip(s["lines"], s["audio"]):
            frames = pcm_frames(audio)
            length = len(frames) / width / rate
            pcm += b"\0" * (int(t * rate) * width - len(pcm))
            pcm += frames
            cues.append((t, t + length, line))
            t += length + GAP
        start += s["duration"]
    pcm += b"\0" * (int(start * rate) * width - len(pcm))
    narration = WORK / "narration.wav"
    with wave.open(str(narration), "wb") as wn:
        wn.setparams(params._replace(nframes=0))
        wn.writeframes(bytes(pcm))

    ass = WORK / "subtitles.ass"
    ass.write_text(
        "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\n\n"
        "[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, "
        "Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, "
        "Alignment, MarginL, MarginR, MarginV, Encoding\n"
        "Style: Default,Inter Medium,40,&H00F3EDE6,&H00FFFFFF,&H00000000,&H64140E0B,0,0,0,0,100,100,0,0,1,0,0,"
        "2,160,160,52,1\n\n"
        "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" % (W, H)
        + "".join(f"Dialogue: 0,{ass_time(a)},{ass_time(b + 0.15)},Default,,0,0,0,,{text}\n" for a, b, text in cues)
    )
    (ASSETS / "intro.srt").write_text("".join(
        f"{i}\n{srt_time(a)} --> {srt_time(b + 0.15)}\n{text}\n\n" for i, (a, b, text) in enumerate(cues, 1)))

    listing = WORK / "clips.txt"
    listing.write_text("".join(f"file '{c}'\n" for c in clips))
    joined = WORK / "joined.mp4"
    run("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", listing, "-c", "copy", joined)
    check_end = sum(s["duration"] for s in SCENES[: [s["id"] for s in SCENES].index("check") + 1]) - 0.5
    render_poster(joined, check_end)
    out = ASSETS / "intro.mp4"
    run("ffmpeg", "-y", "-loglevel", "error", "-i", joined, "-i", narration,
        "-vf", f"ass={ass}", "-c:v", "libx264", "-preset", "slow", "-crf", "26", "-pix_fmt", "yuv420p",
        "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", "-shortest", out)
    print(f"wrote {out} ({duration(out):.1f}s, {out.stat().st_size / 1e6:.1f} MB)")


if __name__ == "__main__":
    main()
