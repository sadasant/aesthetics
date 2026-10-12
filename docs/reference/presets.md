# Presets reference

Six presets ship with Aesthetics. None is active unless you name it in `extends`, in a rule, or with `--use`. Identifiers are stable, but the wording of guidelines may improve between versions. `aesthetics rules <preset>` prints the definitions in your installed version, including remedies.

Each guideline applies only to the artifacts its scope names, so activating a preset never evaluates unrelated files.

| Preset | Foundation | Applies to |
| --- | --- | --- |
| [`unix`](#unix) | The Unix philosophy (McIlroy, Thompson, Pike, Raymond) | file, directory, repository, diff |
| [`kiss`](#kiss) | The KISS principle ("Keep it simple, stupid", Kelly Johnson) | file, directory, diff |
| [`atomic`](#atomic) | Atomic commits and focused changes | diff |
| [`readme`](#readme) | Standard Readme (Richard Littauer) | file, repository |
| [`diataxis`](#diataxis) | The Diátaxis documentation framework (Daniele Procida) | file, directory, repository |
| [`solid`](#solid) | The SOLID principles of object-oriented design (Robert C. Martin) | file, directory, repository |

## unix

**Unix Philosophy.** Focused responsibilities, simple interfaces, composability, and minimal mechanisms.

Foundation: The Unix philosophy (McIlroy, Thompson, Pike, Raymond). <https://en.wikipedia.org/wiki/Unix_philosophy> · <http://www.catb.org/~esr/writings/taoup/html/ch01s06.html>

- **`unix/do-one-thing`** · file · code · warning  
  The file has one focused responsibility, and everything in it serves that single purpose.
- **`unix/simple-interfaces`** · file · code · warning  
  What the file exposes to other code is small and simple: few entry points, plain data in and out, and no hidden coupling through global or shared mutable state.
- **`unix/composable`** · file, directory · code · warning  
  Components can be combined with others: they take inputs and produce outputs instead of reaching into their surroundings, and they make no assumptions about who calls them or what runs next.
- **`unix/minimal-mechanism`** · file, diff · code · warning  
  The code uses the least mechanism that does the job; it adds no framework, layer, plugin system, or configuration knob that the problem does not need.
- **`unix/thin-entry-point`** · file · code · `main.*`, `__main__.py`, `**/cmd/**/*.go` · warning  
  The entry point only starts the program: it reads input and configuration, wires components together, and delegates, while the real work lives in reusable modules.
- **`unix/deliberate-public-surface`** · file · `index.{ts,tsx,js,jsx,mjs,cjs}`, `lib.rs`, `mod.rs`, `__init__.py`, `mod.ts` · warning  
  The module index exposes a deliberate, minimal public surface: it exports what callers need, hides internals, and makes the module's boundary obvious.
- **`unix/manifest-interface`** · file · `package.json`, `Cargo.toml`, `pyproject.toml`, `go.mod`, `composer.json`, `*.gemspec`, `deno.json` · warning  
  The package manifest is deliberate: it states the package's identity and public entry points, and contains nothing accidental such as leftover scripts, development tools listed as runtime dependencies, or install-time side effects.
- **`unix/focused-directories`** · directory · code · warning  
  The directory has one clear purpose that its name conveys: the files directly in it serve that purpose, and a directory that only holds subdirectories groups them under a shared, conventional role, such as cmd, internal, or packages.
- **`unix/clear-boundaries`** · repository · warning  
  The repository is composed of focused components with narrow, clear boundaries; a reader can state the purpose of each top-level component in one sentence.

## kiss

**Keep It Simple.** Directness and avoidance of unnecessary complexity.

Foundation: The KISS principle ("Keep it simple, stupid", Kelly Johnson). <https://en.wikipedia.org/wiki/KISS_principle>

- **`kiss/direct`** · file · code · warning  
  The code solves its problem directly; a reader can follow the main path without decoding indirection, cleverness, or unnecessary generality.
- **`kiss/no-speculative-generality`** · file, diff · code · warning  
  The code contains no abstractions, parameters, options, or extension points built for needs that do not exist yet.
  *In tension with `solid/open-closed`, `solid/depend-on-abstractions`.*
- **`kiss/simplest-change`** · diff · warning  
  The change reaches its goal with the simplest adequate approach and adds no complexity beyond what the goal requires.
- **`kiss/readable-tests`** · file · test · warning  
  Each test reads as a short, direct example: obvious setup, one behavior, and a clear expectation, without elaborate helpers or indirection hiding what is checked.
- **`kiss/simple-configuration`** · file · config · warning  
  The configuration states only what differs from sensible defaults, and its structure is flat and easy to read.
- **`kiss/shallow-structure`** · directory · warning  
  The directory layout is shallow and unsurprising; nesting and splitting exist only where they make things easier to find.

## atomic

**Atomic Changes.** Focused, coherent changes without unrelated modifications.

Foundation: Atomic commits and focused changes. <https://en.wikipedia.org/wiki/Atomic_commit#Revision_control>

- **`atomic/single-purpose`** · diff · error  
  The change has one coherent purpose that can be summarized in a single sentence.
- **`atomic/no-unrelated-modifications`** · diff · error  
  The change contains no unrelated edits such as drive-by refactors, reformatting, renames, or fixes that do not serve its purpose.
- **`atomic/self-contained`** · diff · warning  
  The change is self-contained: it includes what its purpose requires, such as tests and documentation updates, and leaves the repository consistent.
- **`atomic/consistent-with-surroundings`** · diff · warning  
  The changed code follows the conventions, naming, and structure of the code around it.
- **`atomic/reviewable`** · diff · warning  
  The change is small enough to review as one unit and could not naturally be split into independent changes.

## readme

**Standard Readme.** Clear purpose, installation, usage, and relevant repository orientation.

Foundation: Standard Readme (Richard Littauer). <https://github.com/RichardLitt/standard-readme>

- **`readme/states-purpose`** · file · `{README,Readme,readme}{,.md,.markdown,.rst,.txt,.adoc,.org}` · error  
  The README opens with the project's name and a short statement of what it is, what problem it solves, and who it is for.
- **`readme/installation`** · file · `{README,Readme,readme}{,.md,.markdown,.rst,.txt,.adoc,.org}` · depth ≤ 0 · warning  
  The README explains how to install or obtain the project, including prerequisites, with commands a reader can copy.
- **`readme/usage`** · file · `{README,Readme,readme}{,.md,.markdown,.rst,.txt,.adoc,.org}` · warning  
  The README shows how to use the project with concrete, minimal examples.
- **`readme/orientation`** · file · `{README,Readme,readme}{,.md,.markdown,.rst,.txt,.adoc,.org}` · depth ≤ 0 · warning  
  The README orients newcomers where it matters: where further documentation lives, how the repository is organized, how to contribute or get help, and the license.
- **`readme/conventional-structure`** · file · `{README,Readme,readme}{,.md,.markdown,.rst,.txt,.adoc,.org}` · warning  
  The README uses clear headings in a conventional order (title, description, install, usage, further sections) and stays concise.
- **`readme/repository-has-readme`** · repository · error  
  The repository has a README at its root that explains what the project is and how to start using it.

## diataxis

**Diátaxis.** Documentation organized according to tutorials, how-to guides, reference, and explanation.

Foundation: The Diátaxis documentation framework (Daniele Procida). <https://diataxis.fr/>

- **`diataxis/one-mode`** · file · docs · warning  
  The document serves one documentation mode (tutorial, how-to guide, reference, or explanation) instead of mixing them.
  *In tension with `readme/orientation`, `readme/usage`.*
- **`diataxis/mode-conventions`** · file · docs · warning  
  The document follows the conventions of the documentation mode it serves.
- **`diataxis/mode-signposted`** · file · docs · info  
  The document's title and opening tell readers what kind of document it is and what they will get from it.
- **`diataxis/organized-by-mode`** · directory · `{docs,doc,documentation,Documentation}` · warning  
  The documentation is organized by mode, so readers can find tutorials, how-to guides, reference, and explanation separately.
- **`diataxis/covers-needs`** · repository · docs · warning  
  The documentation as a whole serves learning, accomplishing tasks, looking facts up, and understanding, or makes clear which of these needs it deliberately leaves out.

## solid

**SOLID.** Cohesion, responsibilities, abstractions, and dependency boundaries.

Foundation: The SOLID principles of object-oriented design (Robert C. Martin). <https://en.wikipedia.org/wiki/SOLID>

- **`solid/single-responsibility`** · file · code · warning  
  Each module and type has one reason to change: its contents serve a single concern or a single group of stakeholders.
- **`solid/open-closed`** · file · code · warning  
  Behavior that is expected to vary can be extended by adding code rather than by editing stable, tested code.
- **`solid/substitutable`** · file · code · warning  
  Implementations honor the contracts of the interfaces and base types they implement; no implementation surprises callers by weakening guarantees or rejecting valid inputs.
- **`solid/interface-segregation`** · file · code · warning  
  Interfaces are narrow and shaped by their clients; no client depends on methods it does not use.
- **`solid/depend-on-abstractions`** · file · code · warning  
  High-level policy depends on abstractions rather than on concrete low-level details, and dependencies are passed in rather than constructed deep inside.
- **`solid/cohesive-packages`** · directory · code · warning  
  The package is cohesive: its parts change together, and it depends on packages that are more stable than itself, never on its own dependents.
- **`solid/dependency-direction`** · repository · code · warning  
  Dependencies between components flow in one direction, toward stable core abstractions, without cycles.
