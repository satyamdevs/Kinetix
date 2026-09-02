import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = {
  title: "Orca Dashboard",
  description: "Usage and latency telemetry for the Orca gateway",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>
        <header className="nav">
          <Link href="/" className="brand">
            Orca
          </Link>
          <nav>
            <Link href="/">Usage</Link>
            <Link href="/latency">Latency</Link>
            <Link href="/policies">Policies</Link>
          </nav>
        </header>
        <main className="container">{children}</main>
      </body>
    </html>
  );
}