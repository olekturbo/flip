"""
Playwright script to capture Flip 7 gameplay screenshots.
Simulates a 3-player game and takes screenshots at key moments.
"""
import asyncio
import time
from playwright.async_api import async_playwright

BASE_URL = "http://localhost:8080"
OUT = "/Users/klaudia/ai/flip/screenshots"


async def enter_name(page, name):
    await page.locator("#name-modal").wait_for(state="visible", timeout=6000)
    await page.fill("#name-input", name)
    await page.click("button.btn-primary")
    await page.locator("#name-modal").wait_for(state="hidden", timeout=6000)


async def take(page, name, full=False):
    path = f"{OUT}/{name}.png"
    await page.screenshot(path=path, full_page=full)
    print(f"  saved {name}.png")


async def main():
    async with async_playwright() as pw:
        browser = await pw.chromium.launch(headless=True)

        # Each player in a separate context (separate localStorage)
        ctx1 = await browser.new_context(viewport={"width": 1280, "height": 800})
        ctx2 = await browser.new_context(viewport={"width": 1280, "height": 800})
        ctx3 = await browser.new_context(viewport={"width": 1280, "height": 800})

        p1 = await ctx1.new_page()
        p2 = await ctx2.new_page()
        p3 = await ctx3.new_page()

        # ── Screenshot: home/lobby page ───────────────────────────────────────
        print("Capturing home page…")
        await p1.goto(BASE_URL)
        await p1.wait_for_load_state("networkidle")
        await take(p1, "01_home")

        # ── Player 1 creates room ─────────────────────────────────────────────
        print("Player 1 creates room…")
        await p1.click("button:has-text('Create Room')")
        await p1.wait_for_url("**/game/**", timeout=8000)
        await enter_name(p1, "Alice")
        await p1.locator("#lobby-overlay").wait_for(state="visible", timeout=8000)
        await asyncio.sleep(0.5)
        await take(p1, "02_lobby_waiting")

        room_url = await p1.input_value("#share-url")
        print(f"  room URL: {room_url}")

        # ── Players 2 & 3 join ────────────────────────────────────────────────
        print("Player 2 joins…")
        await p2.goto(room_url)
        await enter_name(p2, "Bob")
        await p2.locator("#lobby-overlay").wait_for(state="visible", timeout=8000)

        print("Player 3 joins…")
        await p3.goto(room_url)
        await enter_name(p3, "Carol")
        await p3.locator("#lobby-overlay").wait_for(state="visible", timeout=8000)

        await asyncio.sleep(1.2)
        await take(p1, "03_lobby_three_players")

        # ── Start game ────────────────────────────────────────────────────────
        print("Starting game…")
        start_btn = p1.locator("#btn-start")
        await start_btn.wait_for(state="visible", timeout=5000)
        await start_btn.click()

        for page in (p1, p2, p3):
            await page.locator("#lobby-overlay").wait_for(state="hidden", timeout=8000)

        await asyncio.sleep(1.5)
        await take(p1, "04_game_start_alice_pov")
        await take(p2, "05_game_start_bob_pov")
        await take(p3, "06_game_start_carol_pov")

        # ── Play through a round by drawing/staying ───────────────────────────
        print("Simulating gameplay…")
        pages_named = [("Alice", p1), ("Bob", p2), ("Carol", p3)]

        async def try_action(page, btn_id, timeout_ms=800):
            btn = page.locator(btn_id)
            try:
                await btn.wait_for(state="visible", timeout=timeout_ms)
                if await btn.is_enabled():
                    await btn.click()
                    return True
            except Exception:
                pass
            return False

        async def handle_action_overlays():
            """Handle any action card targeting overlays that appear."""
            for page in (p1, p2, p3):
                for overlay_id, btn_sel in [
                    ("action-overlay", ".target-btn"),
                    ("thief-target-overlay", ".target-btn"),
                    ("thief-card-overlay", ".thief-card-btn"),
                ]:
                    try:
                        overlay = page.locator(f"#{overlay_id}")
                        visible = await overlay.is_visible()
                        if visible:
                            await take(page, f"action_overlay_{overlay_id}_{int(time.time())}")
                            btns = await page.locator(btn_sel).all()
                            if btns:
                                await btns[0].click()
                                await asyncio.sleep(0.6)
                    except Exception:
                        pass

        # Draw a bunch of cards
        for i in range(12):
            acted = False
            for pname, page in pages_named:
                if await try_action(page, "#btn-draw", 600):
                    acted = True
                    await asyncio.sleep(0.6)
                    await handle_action_overlays()
                    break
            if not acted:
                await asyncio.sleep(0.3)

        await asyncio.sleep(1)
        await take(p1, "07_mid_game_alice")
        await take(p2, "08_mid_game_bob")
        await take(p3, "09_mid_game_carol")

        # Have first player who can, Stay
        for pname, page in pages_named:
            if await try_action(page, "#btn-stay", 600):
                await asyncio.sleep(1)
                await take(page, f"10_stay_{pname.lower()}")
                break

        # Play out rest of round
        for _attempt in range(60):
            # check if round ended
            goto_next_round = False
            for page in (p1, p2, p3):
                try:
                    el = page.locator("#round-end-overlay")
                    if await el.is_visible():
                        await take(p1, "11_round_end_scores")
                        goto_next_round = True
                        break
                except Exception:
                    pass

            if goto_next_round:
                break

            for pname, page in pages_named:
                for btn_id in ("#btn-draw", "#btn-stay"):
                    if await try_action(page, btn_id, 400):
                        await asyncio.sleep(0.4)
                        await handle_action_overlays()
                        break

        # Wait a moment for round-end overlay if not yet shown
        await asyncio.sleep(1.5)
        for page in (p1, p2, p3):
            try:
                el = page.locator("#round-end-overlay")
                if await el.is_visible():
                    await take(p1, "11_round_end_scores")
                    break
            except Exception:
                pass

        # ── Rules modal ───────────────────────────────────────────────────────
        print("Opening rules modal…")
        await p1.click("button:has-text('Rules')")
        await asyncio.sleep(0.8)
        await take(p1, "12_rules_modal")
        # scroll down in rules modal
        rules_modal = p1.locator("#rules-modal")
        await rules_modal.evaluate("el => el.scrollTop = 600")
        await asyncio.sleep(0.4)
        await take(p1, "13_rules_modal_scrolled")

        await browser.close()
        print("\nAll screenshots saved to:", OUT)


asyncio.run(main())
