import { verifyConsumer } from "@qqdevelopments/prompt-shield-web-core/distribution";
const metadata = await verifyConsumer(process.cwd());
console.log("Verified shared frontend archive " + metadata.version);
