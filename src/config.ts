import { config as loadEnv } from "dotenv";

loadEnv();

function requireEnv(name: string): string {
  const value = process.env[name];
  if (value === undefined || value.trim() === "") {
    throw new Error(`Missing or empty environment variable: ${name}`);
  }
  return value;
}

export const config = {
  botToken: requireEnv("BOT_TOKEN"),
  nodeEnv: process.env.NODE_ENV ?? "development",
  botApiRoot: process.env.BOT_API_ROOT,
} as const;
