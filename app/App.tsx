import { ThemeToggle } from "@/theme/ThemeToggle";
import { useVersion } from "@/version/useVersion";

export function App() {
  const version = useVersion();

  return (
    <div className="flex min-h-screen flex-col bg-surface text-ink">
      <header className="flex justify-end p-6">
        <ThemeToggle />
      </header>
      <main className="grid flex-1 place-items-center p-8">
        <section
          aria-labelledby="app-title"
          className="w-full max-w-2xl rounded-3xl border border-line bg-panel p-8 shadow-xl sm:p-16"
        >
          <p className="mb-4 text-xs font-bold tracking-[0.16em] text-accent uppercase">
            Web App Template
          </p>
          <h1
            className="text-[clamp(2.5rem,8vw,5rem)] leading-[0.95] font-semibold tracking-[-0.04em]"
            id="app-title"
          >
            Your application starts here.
          </h1>
          <p className="mt-7 max-w-xl text-lg leading-7 text-muted">
            React, Go, PostgreSQL, and the development tools needed to ship them
            together.
          </p>
        </section>
      </main>
      <footer className="p-6 text-center text-xs text-muted">
        {version && <p>Version {version}</p>}
      </footer>
    </div>
  );
}
