'use strict';
const {createOtelStageDiagnostics}=require('./otel-stages.cjs');
// Read once at process/module initialization. No request headers or query flags.
const diagnostic=createOtelStageDiagnostics({enabled:process.env.AI_GATEWAY_OTEL_STAGE_DIAGNOSTICS==='1'});
module.exports={run:fn=>diagnostic.run(fn),stage:(name,fn)=>diagnostic.stage(name,fn),snapshot:()=>diagnostic.snapshot()};
