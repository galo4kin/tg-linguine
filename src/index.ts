import { Bot } from "grammy";
import { config } from "./config.js";

const bot = config.botApiRoot
  ? new Bot(config.botToken, { client: { apiRoot: config.botApiRoot } })
  : new Bot(config.botToken);

bot.command("start", async (ctx) => {
  await ctx.reply(
    "Привет! Я tg-linguine — бот для изучения иностранных языков.\n\n" +
      "Пока я только запущен: дальше здесь появятся уроки и упражнения.",
  );
});

bot.catch((err) => {
  console.error("Ошибка бота:", err);
});

await bot.start();
