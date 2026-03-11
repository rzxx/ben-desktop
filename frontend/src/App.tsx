import { type FormEvent, useState } from "react";
import { GreetService } from "../bindings/changeme";

function App() {
  const [name, setName] = useState("");
  const [result, setResult] = useState(
    "Call into the Go service from a cleaned-up React shell.",
  );
  const [isLoading, setIsLoading] = useState(false);

  const doGreet = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setIsLoading(true);

    try {
      const targetName = name.trim() || "anonymous";
      const nextResult = await GreetService.Greet(targetName);
      setResult(nextResult);
    } catch (error) {
      console.error(error);
      setResult("The Go service did not respond as expected.");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <main className="min-h-screen overflow-hidden bg-stone-950 px-6 py-10 text-stone-50">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-5xl items-center">
        <div className="grid w-full gap-6 lg:grid-cols-[1.2fr_0.8fr]">
          <section className="relative overflow-hidden rounded-[2rem] border border-white/10 bg-white/6 p-8 shadow-[0_30px_120px_rgba(15,23,42,0.45)] backdrop-blur">
            <div className="absolute inset-x-10 top-0 h-px bg-gradient-to-r from-transparent via-amber-200/80 to-transparent" />
            <p className="text-xs tracking-[0.35em] text-amber-200/80 uppercase">
              Wails 3 + React + Bun
            </p>
            <h1 className="mt-6 max-w-xl text-4xl leading-tight font-semibold text-balance text-stone-50 sm:text-5xl">
              The default Vite scaffold is gone. The app shell is yours again.
            </h1>
            <p className="mt-5 max-w-2xl text-base leading-7 text-stone-300">
              Tailwind is wired through Vite, Bun owns the frontend workflow,
              and the starter assets have been removed so this project can start
              from a clean baseline.
            </p>

            <div className="mt-8 grid gap-3 text-sm text-stone-200 sm:grid-cols-3">
              <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
                <p className="text-stone-400">Package manager</p>
                <p className="mt-2 font-medium">Bun</p>
              </div>
              <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
                <p className="text-stone-400">Styling</p>
                <p className="mt-2 font-medium">Tailwind CSS v4</p>
              </div>
              <div className="rounded-2xl border border-white/10 bg-black/20 p-4">
                <p className="text-stone-400">Quality</p>
                <p className="mt-2 font-medium">ESLint + Prettier</p>
              </div>
            </div>
          </section>

          <section className="rounded-[2rem] border border-white/10 bg-stone-950/70 p-8 shadow-[0_24px_80px_rgba(0,0,0,0.45)] backdrop-blur">
            <p className="text-sm tracking-[0.28em] text-stone-400 uppercase">
              Backend check
            </p>
            <form className="mt-6 space-y-4" onSubmit={doGreet}>
              <label className="block">
                <span className="mb-2 block text-sm text-stone-300">Name</span>
                <input
                  autoComplete="off"
                  className="w-full rounded-2xl border border-white/10 bg-white/6 px-4 py-3 text-base text-stone-50 transition outline-none focus:border-amber-200/60 focus:bg-white/10"
                  onChange={(event) => setName(event.target.value)}
                  placeholder="Type a name"
                  type="text"
                  value={name}
                />
              </label>

              <button
                className="inline-flex w-full items-center justify-center rounded-2xl bg-amber-200 px-4 py-3 text-sm font-semibold text-stone-950 transition hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-70"
                disabled={isLoading}
                type="submit"
              >
                {isLoading ? "Calling service..." : "Run greet service"}
              </button>
            </form>

            <div className="mt-6 rounded-2xl border border-white/10 bg-black/20 p-4">
              <p className="text-xs tracking-[0.24em] text-stone-500 uppercase">
                Response
              </p>
              <p className="mt-3 text-sm leading-6 text-stone-200">{result}</p>
            </div>
          </section>
        </div>
      </div>
    </main>
  );
}

export default App;
