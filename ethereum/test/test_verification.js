const BigNumber = web3.BigNumber;
const {
  deployLightClientBridge, signatureSubstrateToEthereum, buildCommitment,
  createMerkleTree, deployGenericAppWithChannels, ChannelId, mine, lockupFunds, catchRevert
} = require("./helpers");
const ETHApp = artifacts.require("ETHApp");
const { keccakFromHexString, keccak } = require("ethereumjs-util");
const { blake2AsHex } = require("@polkadot/util-crypto");
const fixture = require('./fixtures/beefy-fixture-data.json');


require("chai")
  .use(require("chai-as-promised"))
  .use(require("chai-bignumber")(BigNumber))
  .should();

const ethers = require("ethers");
const { expect } = require("chai");

contract("IncentivizedInboundChannel", function (accounts) {
  // Accounts
  const owner = accounts[0];
  const userOne = accounts[1];
  const userTwo = accounts[2];
  const userThree = accounts[3];

  describe("initialize LightClientBridge", function () {
    beforeEach(async function () {
      const validatorsMerkleTree = createMerkleTree(["0xE04CC55ebEE1cBCE552f250e85c57B70B2E2625b", "0x25451A4de12dcCc2D166922fA938E900fCc4ED24"]);
      this.validatorsLeaf0 = validatorsMerkleTree.getHexLeaves()[0];
      this.validatorsLeaf1 = validatorsMerkleTree.getHexLeaves()[1];
      this.validator0PubKeyMerkleProof = validatorsMerkleTree.getHexProof(this.validatorsLeaf0);
      this.validator1PubKeyMerkleProof = validatorsMerkleTree.getHexProof(this.validatorsLeaf1);

      this.lightClientBridge = await deployLightClientBridge(validatorsMerkleTree.getHexRoot());
      const newCommitment = await this.lightClientBridge.newSignatureCommitment(
        fixture.commitmentHash,
        fixture.bitfield,
        signatureSubstrateToEthereum(fixture.signature0),
        0,
        "0xE04CC55ebEE1cBCE552f250e85c57B70B2E2625b",
        this.validator0PubKeyMerkleProof
      );

      const lastId = (await this.lightClientBridge.currentId()).sub(new web3.utils.BN(1));

      await catchRevert(this.lightClientBridge.validatorBitfield(lastId), 'Error: Block wait period not over');

      await mine(45);

      const bitfield = await this.lightClientBridge.validatorBitfield(lastId);
      expect(printBitfield(bitfield)).to.eq('10')

      const completeCommitment = await this.lightClientBridge.completeSignatureCommitment(
        lastId,
        fixture.commitmentHash,
        fixture.commitment,
        [signatureSubstrateToEthereum(fixture.signature1)],
        [1],
        ["0x25451A4de12dcCc2D166922fA938E900fCc4ED24"],
        [this.validator1PubKeyMerkleProof]
      );
      console.log(await this.lightClientBridge.latestMMRRoot());
      [channels, this.ethApp] = await deployGenericAppWithChannels(owner, this.lightClientBridge.address, ETHApp);
      this.inbound = channels.incentivized.inbound;
      this.POLKADOT_ADDRESS = "38j4dG5GzsL1bw2U2AVgeyAk6QTxq43V7zPbdXAmbVLjvDCK"
      await lockupFunds(this.ethApp, userOne, this.POLKADOT_ADDRESS, 5000, ChannelId.Incentivized);
    });

    it("should successfully verify a commitment", async function () {
      const abi = this.ethApp.abi;
      const iChannel = new ethers.utils.Interface(abi);
      const polkadotSender = ethers.utils.formatBytes32String('fake-polkadot-address');
      const unlockFragment = iChannel.functions['unlock(bytes32,address,uint256)'];
      const payloadOne = iChannel.encodeFunctionData(unlockFragment, [polkadotSender, userTwo, 2]);
      const messageOne = {
        target: this.ethApp.address,
        nonce: 1,
        payload: payloadOne
      };
      const payloadTwo = iChannel.encodeFunctionData(unlockFragment, [polkadotSender, userThree, 5]);
      const messageTwo = {
        target: this.ethApp.address,
        nonce: 2,
        payload: payloadTwo
      };
      const messages = [messageOne, messageTwo];
      const commitment = buildCommitment(messages);
      const tx = await this.inbound.submit(
        messages,
        commitment,
        fixture.leaf,
        fixture.leafIndex,
        fixture.leafCount,
        fixture.proofs,
        { from: userOne }
      );
      console.log(tx);
    });
  });
});

function parseBitfield(s) {
  return parseInt(s, 2)
}

function printBitfield(s) {
  return parseInt(s.toString(), 10).toString(2)
}
